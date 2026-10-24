package miner

import (
	"fmt"
	"strconv"

	"salad-bzminer-worker/internal/config"
	"salad-bzminer-worker/internal/gpu"
)

// Verified against BzMiner v100.50 --help. Do not add a flag that is not in
// that output.
const (
	flagAlgorithm = "-a"
	flagWallet    = "-w"
	flagPool      = "-p"
	flagWorker    = "--worker"
	flagCPU       = "--cpu"
	flagNVIDIA    = "--nvidia"
	flagHTTPOn    = "--http_enabled"
	flagHTTPAddr  = "--http_address"
	flagHTTPPort  = "--http_port"
	flagLogLevel  = "--log-level"
	flagOutput    = "-o"
	flagNoColor   = "--no-color"
	flagNoUDP     = "--disable_udp"
	flagAutoUpd   = "--auto-update"
	flagPluginUpd = "--plugin-update"
	flagSet       = "--set"
)

// Build returns the exact argv used to start BzMiner. No shell is involved.
func Build(cfg *config.Config, workerName string) ([]string, error) {
	if cfg.MinerBin == "" {
		return nil, fmt.Errorf("miner: BZMINER_BIN is empty")
	}
	if workerName == "" {
		return nil, fmt.Errorf("miner: worker name is empty")
	}
	argv := []string{
		cfg.MinerBin,
		flagAlgorithm, cfg.Algorithm,
		flagWallet, cfg.WalletArg,
		flagPool, cfg.Pool,
	}
	if cfg.PoolFallback != "" {
		argv = append(argv, flagPool, cfg.PoolFallback)
	}
	argv = append(argv,
		flagWorker, workerName,
		flagNVIDIA,
	)
	if cfg.CPUEnabled() {
		argv = append(argv, flagCPU)
	} else {
		argv = append(argv, flagCPU, "0")
	}
	if cfg.HealthEnabled {
		argv = append(argv,
			flagHTTPOn, "true",
			flagHTTPAddr, "127.0.0.1",
			flagHTTPPort, strconv.Itoa(cfg.MinerHTTPPort),
		)
	} else {
		argv = append(argv, flagHTTPOn, "false")
	}
	argv = append(argv,
		flagLogLevel, cfg.LogLevel,
		flagOutput, "log",
		flagNoColor,
		flagNoUDP,
		flagAutoUpd, "off",
		flagPluginUpd, "off",
		// BzMiner's share-based stall guard can restart mining before a slow
		// GPU has had a realistic chance to find its first share. The
		// supervisor only restarts on process exit.
		flagSet, "stall_guard.enabled=false",
	)
	argv = append(argv, cfg.ExtraArgs...)
	return argv, nil
}

// GPU is accepted so callers cannot accidentally introduce CUDA indexes; the
// command never names a device.
func GPU(_ gpu.GPU) {}
