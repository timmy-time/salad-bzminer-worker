package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"syscall"
	"time"

	"salad-bzminer-worker/internal/config"
	"salad-bzminer-worker/internal/server"
)

const stableRun = 5 * time.Minute

// Delay computes the restart delay: base * 2^attempt, capped at max, with
// ±20% jitter. attempt is zero-based.
func Delay(attempt int, base, max time.Duration, jitter func() float64) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if max < base {
		max = base
	}
	d := base
	for range attempt {
		if d >= max {
			break
		}
		d *= 2
	}
	if d > max {
		d = max
	}
	f := 0.5
	if jitter != nil {
		f = jitter()
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	out := time.Duration(float64(d) * (0.8 + 0.4*f))
	if out < time.Millisecond {
		out = time.Millisecond
	}
	return out
}

// Run starts the miner and supervises it until ctx is cancelled. A child exit
// is always recoverable and restarts with capped exponential backoff; an exec
// failure is fatal.
func Run(ctx context.Context, cfg *config.Config, argv []string, st *server.State, log *slog.Logger) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		started := time.Now()
		if err := cmd.Start(); err != nil {
			st.SetPhase(server.PhaseFatal)
			return fmt.Errorf("failed to start miner: %w", err)
		}
		st.SetMinerRunning(cmd.Process.Pid)
		log.Info("miner started", "pid", cmd.Process.Pid, "worker", st.View().WorkerName)

		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()

		select {
		case <-ctx.Done():
			st.SetPhase(server.PhaseStopping)
			log.Info("shutdown requested")
			stopChild(cmd.Process, waitCh, cfg.ShutdownGrace, log)
			st.SetMinerStopped(server.ExitInfo{Code: 0, At: time.Now()})
			return nil
		case err := <-waitCh:
			info := exitInfo(cmd.ProcessState, err)
			st.SetMinerStopped(info)
			st.IncRestarts()
			st.SetPhase(server.PhaseBackoff)
			log.Warn("miner exited", "code", info.Code, "signal", info.Signal, "ran", time.Since(started).String())

			if ctx.Err() != nil {
				return nil
			}
			if time.Since(started) >= stableRun {
				attempt = 0
			}
			delay := Delay(attempt, cfg.BackoffBase, cfg.BackoffMax, rand.Float64)
			attempt++
			log.Info("restarting miner", "delay", delay.String(), "attempt", attempt)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
}

func stopChild(proc *os.Process, wait <-chan error, grace time.Duration, log *slog.Logger) {
	_ = syscall.Kill(-proc.Pid, syscall.SIGTERM)
	select {
	case <-wait:
		return
	case <-time.After(grace):
		log.Warn("miner did not exit after grace period; killing", "grace", grace.String())
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
		<-wait
	}
}

func exitInfo(ps *os.ProcessState, err error) server.ExitInfo {
	info := server.ExitInfo{At: time.Now()}
	if ps == nil {
		if err != nil {
			info.Code = -1
			info.Signal = err.Error()
		}
		return info
	}
	info.Code = ps.ExitCode()
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		info.Signal = ws.Signal().String()
		info.Code = -1
	}
	return info
}
