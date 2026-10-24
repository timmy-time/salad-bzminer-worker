package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"salad-bzminer-worker/internal/bzapi"
	"salad-bzminer-worker/internal/config"
	"salad-bzminer-worker/internal/gpu"
	"salad-bzminer-worker/internal/identity"
	"salad-bzminer-worker/internal/miner"
	"salad-bzminer-worker/internal/server"
	"salad-bzminer-worker/internal/supervisor"
	"salad-bzminer-worker/internal/version"
)

const usage = `Usage: worker [command]

Commands:
  run             Run BzMiner under supervision (default)
  print-config    Print resolved configuration, GPU identity, and argv as JSON
  print-command   Print the worker name and BzMiner argv as JSON
  healthcheck     Check the local health endpoint (for Docker/Salad probes)
  version         Print version information
`

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "run":
		return runWorker()
	case "print-config":
		return printConfig()
	case "print-command":
		return printCommand()
	case "healthcheck":
		return healthcheck()
	case "version", "-V", "--version":
		fmt.Printf("%s %s (revision %s, built %s, bzminer %s)\n",
			version.Name, version.Version, version.Revision, version.BuildDate, version.BzMinerVersion)
		return 0
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s", cmd, usage)
		return 2
	}
}

func loadConfig() (*config.Config, error) {
	return config.Load(os.LookupEnv)
}

func loggerFor(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func detectGPU(ctx context.Context, cfg *config.Config) (gpu.Result, error) {
	return gpu.Detect(ctx, gpu.Options{
		NvidiaSmiPath:  cfg.NvidiaSmiPath,
		BzMinerPath:    cfg.MinerBin,
		VisibleDevices: os.Getenv("NVIDIA_VISIBLE_DEVICES"),
		Timeout:        45 * time.Second,
	})
}

func workerName(cfg *config.Config, g gpu.Result) string {
	if cfg.WorkerName != "" {
		return cfg.WorkerName
	}
	host, _ := os.Hostname()
	src := identity.ResolveSource(os.LookupEnv, host, g.GPUs)
	return identity.Build(cfg.WorkerPrefix, g.GPUs[0].Name, identity.Suffix(src))
}

func checkMinerBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("BZMINER_BIN: %w", err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("BZMINER_BIN: %s is not executable", path)
	}
	return nil
}

func runWorker() int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	log := loggerFor(cfg.LogLevel)
	st := server.NewState()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var srv *http.Server
	if cfg.HealthEnabled {
		ln, err := net.Listen("tcp", server.Addr(cfg))
		if err != nil {
			log.Error("health server bind failed", "addr", server.Addr(cfg), "error", err)
			return 1
		}
		srv = server.New(cfg, st, version.Version, version.Revision, version.BzMinerVersion)
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("health server stopped", "error", err)
			}
		}()
		log.Info("health server listening", "addr", ln.Addr().String())
	}
	shutdownServer := func() {
		if srv == nil {
			return
		}
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}

	if err := checkMinerBinary(cfg.MinerBin); err != nil {
		st.SetPhase(server.PhaseFatal)
		log.Error("miner binary check failed", "error", err)
		shutdownServer()
		return 1
	}

	res, err := detectGPU(ctx, cfg)
	if err != nil {
		st.SetPhase(server.PhaseFatal)
		log.Error("GPU detection failed", "error", err)
		shutdownServer()
		return 1
	}
	st.SetGPUs(res.GPUs, res.Source)
	for _, g := range res.GPUs {
		log.Info("GPU detected", "index", g.Index, "model", g.Name, "memory_mib", g.MemoryBytes/(1024*1024), "source", res.Source)
	}

	name := workerName(cfg, res)
	st.SetWorker(name)
	argv, err := miner.Build(cfg, name)
	if err != nil {
		st.SetPhase(server.PhaseFatal)
		log.Error("miner command build failed", "error", err)
		shutdownServer()
		return 1
	}
	log.Info("miner command", "worker", name, "argv", strings.Join(maskArgv(argv), " "))

	if cfg.HealthEnabled {
		st.SetAPISupported(true)
		client := bzapi.New("http://127.0.0.1:"+strconv.Itoa(cfg.MinerHTTPPort), 5*time.Second)
		pollCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go pollAPI(pollCtx, client, st, log)
	}

	if err := supervisor.Run(ctx, cfg, argv, st, log); err != nil {
		log.Error("supervisor stopped", "error", err)
		shutdownServer()
		return 1
	}
	shutdownServer()
	log.Info("worker stopped")
	return 0
}

func pollAPI(ctx context.Context, client *bzapi.Client, st *server.State, log *slog.Logger) {
	ticker := time.NewTicker(client.Interval)
	defer ticker.Stop()
	for {
		snap, err := client.Fetch(ctx)
		if err != nil {
			if ctx.Err() == nil {
				st.SetAPIError(err)
				log.Debug("bzminer API unavailable", "error", err)
			}
		} else {
			st.SetAPISuccess(snap)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func maskArgv(argv []string) []string {
	out := append([]string(nil), argv...)
	for i := range len(out) - 1 {
		if out[i] == "-w" || out[i] == "--wallet" {
			out[i+1] = maskWallet(out[i+1])
		}
	}
	return out
}

func maskWallet(s string) string {
	if len(s) <= 14 {
		return "***"
	}
	return s[:10] + "..." + s[len(s)-4:]
}

func printConfig() int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	res, err := detectGPU(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	name := workerName(cfg, res)
	argv, err := miner.Build(cfg, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	doc := map[string]any{
		"algorithm":      cfg.Algorithm,
		"pool":           cfg.Pool,
		"pool_fallback":  cfg.PoolFallback,
		"wallet":         cfg.Wallet,
		"nock_wallet":    cfg.NockWallet,
		"wallet_arg":     cfg.WalletArg,
		"worker_name":    name,
		"worker_prefix":  cfg.WorkerPrefix,
		"gpu_only":       cfg.GPUOnly,
		"cpu_mining":     cfg.CPUMining,
		"gpus":           res.GPUs,
		"gpu_source":     res.Source,
		"health_enabled": cfg.HealthEnabled,
		"health_port":    cfg.HealthPort,
		"log_level":      cfg.LogLevel,
		"argv":           argv,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func printCommand() int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	res, err := detectGPU(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	name := workerName(cfg, res)
	argv, err := miner.Build(cfg, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	doc := map[string]any{"worker_name": name, "argv": argv}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func healthcheck() int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	if !cfg.HealthEnabled {
		return 0
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(cfg.HealthPort) + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: HTTP %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
