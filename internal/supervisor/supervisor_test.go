package supervisor

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"salad-bzminer-worker/internal/config"
	"salad-bzminer-worker/internal/server"
)

func testConfig() *config.Config {
	return &config.Config{
		BackoffBase:   20 * time.Millisecond,
		BackoffMax:    80 * time.Millisecond,
		ShutdownGrace: 500 * time.Millisecond,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDelayBounds(t *testing.T) {
	base := 2 * time.Second
	max := 10 * time.Second
	for attempt := range 10 {
		lo := Delay(attempt, base, max, func() float64 { return 0 })
		hi := Delay(attempt, base, max, func() float64 { return 1 })
		if lo < 0 || hi > 12*time.Second {
			t.Fatalf("attempt %d: lo=%s hi=%s", attempt, lo, hi)
		}
		if lo > hi {
			t.Fatalf("attempt %d: lo > hi", attempt)
		}
	}
	if Delay(0, base, max, func() float64 { return 0 }) < 1500*time.Millisecond {
		t.Fatal("base delay too small")
	}
	if Delay(20, base, max, func() float64 { return 1 }) > 12*time.Second {
		t.Fatal("max delay not capped")
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	mode := os.Getenv("HELPER_MODE")
	switch mode {
	case "sleep":
		time.Sleep(time.Hour)
	case "crash":
		os.Exit(3)
	}
	os.Exit(0)
}

func TestRunRestartsOnCrash(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_MODE", "crash")
	st := server.NewState()
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	argv := []string{os.Args[0], "-test.run=TestHelperProcess", "--"}
	err := Run(ctx, testConfig(), argv, st, discardLogger())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	v := st.View()
	if v.Restarts < 2 {
		t.Fatalf("restarts = %d, want >= 2", v.Restarts)
	}
	if v.MinerRunning {
		t.Fatal("miner still marked running")
	}
}

func TestRunShutdown(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_MODE", "sleep")
	st := server.NewState()
	ctx, cancel := context.WithCancel(t.Context())
	argv := []string{os.Args[0], "-test.run=TestHelperProcess", "--"}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, testConfig(), argv, st, discardLogger()) }()

	deadline := time.Now().Add(time.Second)
	for !st.View().MinerRunning && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !st.View().MinerRunning {
		t.Fatal("miner did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if st.View().MinerRunning {
		t.Fatal("miner still marked running")
	}
}

func TestRunSpawnFailure(t *testing.T) {
	st := server.NewState()
	err := Run(t.Context(), testConfig(), []string{"/nonexistent/bzminer"}, st, discardLogger())
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	if st.View().Phase != server.PhaseFatal {
		t.Fatalf("phase = %q", st.View().Phase)
	}
}

func TestRunCancelWhileBackoff(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_MODE", "crash")
	st := server.NewState()
	cfg := testConfig()
	cfg.BackoffBase = time.Second
	cfg.BackoffMax = time.Second
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, []string{os.Args[0], "-test.run=TestHelperProcess", "--"}, st, discardLogger())
	}()
	deadline := time.Now().Add(time.Second)
	for st.View().Restarts == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
}
