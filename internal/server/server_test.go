package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"salad-bzminer-worker/internal/bzapi"
	"salad-bzminer-worker/internal/config"
	"salad-bzminer-worker/internal/gpu"
)

func testConfig() *config.Config {
	return &config.Config{
		Algorithm:      "pearl",
		HealthPort:     8080,
		StartupTimeout: 30 * time.Second,
	}
}

func testState() *State {
	st := NewState()
	st.SetGPUs([]gpu.GPU{
		{Index: 0, Name: "NVIDIA GeForce RTX 4090", MemoryBytes: 24 * 1024 * 1024 * 1024},
	}, "bzminer")
	st.SetWorker("salad-rtx4090-1a2b3c4d")
	return st
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthzPhases(t *testing.T) {
	st := testState()
	h := Handler(testConfig(), st, "1.0.0", "abc", "v100.50")
	if rec := get(t, h, "/healthz"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"starting"`) {
		t.Fatalf("starting: %d %s", rec.Code, rec.Body.String())
	}
	st.SetPhase(PhaseBackoff)
	if rec := get(t, h, "/healthz"); rec.Code != 200 {
		t.Fatalf("backoff: %d", rec.Code)
	}
	st.SetPhase(PhaseStopping)
	if rec := get(t, h, "/healthz"); rec.Code != 503 {
		t.Fatalf("stopping: %d", rec.Code)
	}
}

func TestReadyzDeadMiner(t *testing.T) {
	st := testState()
	st.SetAPISupported(true)
	st.SetMinerRunning(123)
	st.SetAPISuccess(&bzapi.Snapshot{
		FetchedAt: time.Now(),
		Pools:     []bzapi.Pool{{Connected: true}},
	})
	if rec := get(t, Handler(testConfig(), st, "1.0.0", "abc", "v100.50"), "/readyz"); rec.Code != 200 {
		t.Fatalf("ready: %d %s", rec.Code, rec.Body.String())
	}
	st.SetMinerStopped(ExitInfo{Code: 1, At: time.Now()})
	rec := get(t, Handler(testConfig(), st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "miner_not_running") {
		t.Fatalf("dead miner: %d %s", rec.Code, rec.Body.String())
	}
}

func TestReadyzStaleAndTimeout(t *testing.T) {
	cfg := testConfig()
	st := testState()
	st.SetAPISupported(true)
	st.SetMinerRunning(123)
	rec := get(t, Handler(cfg, st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "miner_starting") {
		t.Fatalf("no api yet: %d %s", rec.Code, rec.Body.String())
	}

	// Backdate the child start past the startup timeout.
	st.mu.Lock()
	st.minerStartedAt = time.Now().Add(-time.Minute)
	st.mu.Unlock()
	rec = get(t, Handler(cfg, st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "startup_timeout") {
		t.Fatalf("timeout: %d %s", rec.Code, rec.Body.String())
	}

	st.SetAPISuccess(&bzapi.Snapshot{FetchedAt: time.Now().Add(-time.Minute)})
	rec = get(t, Handler(cfg, st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "miner_api_unreachable") {
		t.Fatalf("stale: %d %s", rec.Code, rec.Body.String())
	}
}

func TestReadyzNoAPI(t *testing.T) {
	st := testState()
	st.SetAPISupported(false)
	st.SetMinerRunning(123)
	rec := get(t, Handler(testConfig(), st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "miner_starting") {
		t.Fatalf("no api starting: %d %s", rec.Code, rec.Body.String())
	}
	st.mu.Lock()
	st.minerStartedAt = time.Now().Add(-time.Minute)
	st.mu.Unlock()
	rec = get(t, Handler(testConfig(), st, "1.0.0", "abc", "v100.50"), "/readyz")
	if rec.Code != 200 {
		t.Fatalf("no api ready: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMetrics(t *testing.T) {
	cfg := testConfig()
	st := testState()
	st.SetAPISupported(true)
	st.SetMinerRunning(4242)
	st.SetAPISuccess(&bzapi.Snapshot{
		FetchedAt: time.Now(),
		Summary: bzapi.Summary{
			TotalHashrate: 1234.5,
			Accepted:      7,
			Rejected:      2,
		},
		Devices: []bzapi.Device{{Index: 0, Hashrate: 1234.5}},
		Pools:   []bzapi.Pool{{Connected: true}},
	})
	rec := get(t, Handler(cfg, st, "1.0.0", "abc123", "v100.50"), "/metrics")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`bzminer_worker_info{version="1.0.0",revision="abc123",worker="salad-rtx4090-1a2b3c4d",algorithm="pearl"} 1`,
		`bzminer_miner_running 1`,
		`bzminer_miner_pid 4242`,
		`bzminer_pool_connected 1`,
		`bzminer_shares_accepted_total 7`,
		`bzminer_shares_rejected_total 2`,
		`bzminer_hashrate_total_hs 1234.500000`,
		`bzminer_hashrate_device_hs{gpu="0"} 1234.500000`,
		`bzminer_gpu_memory_total_bytes{gpu="0",model="NVIDIA GeForce RTX 4090"} 25769803776`,
		`bzminer_bzminer_version_info{bzminer_version="v100.50"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if strings.Contains(body, "prl1") {
		t.Error("metrics leaked the wallet")
	}
}

func TestMetricsDeadMiner(t *testing.T) {
	st := testState()
	st.SetAPISupported(false)
	st.SetMinerRunning(1)
	st.SetMinerStopped(ExitInfo{Code: 1})
	rec := get(t, Handler(testConfig(), st, "1.0.0", "abc", "v100.50"), "/metrics")
	body := rec.Body.String()
	if !strings.Contains(body, "bzminer_miner_running 0") {
		t.Errorf("dead miner not reflected: %s", body)
	}
	if strings.Contains(body, "bzminer_miner_api_up") {
		t.Error("API series emitted when API unsupported")
	}
}

func TestMethodsAndNotFound(t *testing.T) {
	h := Handler(testConfig(), testState(), "1.0.0", "abc", "v100.50")
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rec.Code)
	}
	rec = get(t, h, "/nope")
	if rec.Code != 404 {
		t.Fatalf("unknown = %d", rec.Code)
	}
}

func TestEscapeLabel(t *testing.T) {
	if got := escapeLabel("a\"b\\c\nd"); got != `a\"b\\c\nd` {
		t.Fatalf("escapeLabel = %q", got)
	}
}
