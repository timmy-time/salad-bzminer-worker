package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"salad-bzminer-worker/internal/bzapi"
	"salad-bzminer-worker/internal/config"
)

type healthBody struct {
	Status  string   `json:"status"`
	Phase   string   `json:"phase,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
	Worker  string   `json:"worker,omitempty"`
}

func Handler(cfg *config.Config, st *State, version, revision, bzVersion string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		v := st.View()
		code := http.StatusOK
		status := "ok"
		if v.Phase == PhaseStopping || v.Phase == PhaseFatal {
			code = http.StatusServiceUnavailable
			status = "error"
		}
		writeJSON(w, code, healthBody{Status: status, Phase: string(v.Phase)})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		v := st.View()
		reasons := v.ReadyReasons(time.Now(), cfg.StartupTimeout)
		if len(reasons) == 0 {
			writeJSON(w, http.StatusOK, healthBody{Status: "ready", Worker: v.WorkerName})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, healthBody{
			Status:  "not_ready",
			Reasons: reasons,
			Worker:  v.WorkerName,
		})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(renderMetrics(cfg, st.View(), version, revision, bzVersion, time.Now())))
	})
	return mux
}

func New(cfg *config.Config, st *State, version, revision, bzVersion string) *http.Server {
	return &http.Server{
		Handler:           Handler(cfg, st, version, revision, bzVersion),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func Addr(cfg *config.Config) string {
	return net.JoinHostPort(cfg.HealthBind, strconv.Itoa(cfg.HealthPort))
}

func writeJSON(w http.ResponseWriter, code int, body healthBody) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func renderMetrics(cfg *config.Config, v View, version, revision, bzVersion string, now time.Time) string {
	total := 0.0
	accepted, rejected := uint64(0), uint64(0)
	connected := false
	apiFresh := false
	if v.Snapshot != nil {
		total = v.Snapshot.Summary.TotalHashrate
		accepted = v.Snapshot.Summary.Accepted
		rejected = v.Snapshot.Summary.Rejected
		connected = v.Snapshot.ConnectedPool()
		apiFresh = !v.LastAPISuccess.IsZero() && now.Sub(v.LastAPISuccess) <= 30*time.Second
	}

	var b []byte
	add := func(format string, args ...any) {
		b = fmt.Appendf(b, format, args...)
	}
	add("# HELP bzminer_worker_info Build and worker identity.\n")
	add("# TYPE bzminer_worker_info gauge\n")
	add("bzminer_worker_info{version=%q,revision=%q,worker=%q,algorithm=%q} 1\n",
		version, revision, v.WorkerName, cfg.Algorithm)
	add("bzminer_bzminer_version_info{bzminer_version=%q} 1\n", bzVersion)
	add("# HELP bzminer_worker_up Worker supervisor is alive.\n")
	add("# TYPE bzminer_worker_up gauge\n")
	up := 1
	if v.Phase == PhaseStopping || v.Phase == PhaseFatal {
		up = 0
	}
	add("bzminer_worker_up %d\n", up)
	add("bzminer_worker_uptime_seconds %.3f\n", now.Sub(v.StartedAt).Seconds())
	add("bzminer_worker_restarts_total %d\n", v.Restarts)
	add("bzminer_gpu_detected %d\n", boolInt(len(v.GPUs) > 0))
	add("bzminer_gpu_count %d\n", len(v.GPUs))
	for _, g := range v.GPUs {
		add("bzminer_gpu_memory_total_bytes{gpu=%q,model=%q} %d\n",
			strconv.Itoa(g.Index), escapeLabel(g.Name), g.MemoryBytes)
	}
	add("bzminer_miner_running %d\n", boolInt(v.MinerRunning))
	add("bzminer_miner_pid %d\n", v.MinerPID)
	if v.MinerRunning {
		add("bzminer_miner_uptime_seconds %.3f\n", now.Sub(v.MinerStartedAt).Seconds())
	} else {
		add("bzminer_miner_uptime_seconds 0\n")
	}
	if v.APISupported {
		add("bzminer_miner_api_up %d\n", boolInt(apiFresh))
		add("bzminer_pool_connected %d\n", boolInt(connected))
		add("bzminer_shares_accepted_total %d\n", accepted)
		add("bzminer_shares_rejected_total %d\n", rejected)
		add("bzminer_hashrate_total_hs %.6f\n", total)
		if !v.LastAPISuccess.IsZero() {
			add("bzminer_last_pool_api_success_timestamp_seconds %.3f\n",
				float64(v.LastAPISuccess.UnixNano())/1e9)
		}
		if v.Snapshot != nil {
			devices := append([]bzapi.Device(nil), v.Snapshot.Devices...)
			sort.Slice(devices, func(i, j int) bool { return devices[i].Index < devices[j].Index })
			for _, d := range devices {
				add("bzminer_hashrate_device_hs{gpu=%q} %.6f\n", strconv.Itoa(d.Index), d.Hashrate)
			}
		}
	}
	return string(b)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func escapeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		switch s[i] {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
