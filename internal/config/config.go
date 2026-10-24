package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"salad-bzminer-worker/internal/identity"
)

type LookupFunc func(string) (string, bool)

type Config struct {
	Algorithm    string
	Pool         string
	PoolFallback string
	Wallet       string
	NockWallet   string
	WalletArg    string

	WorkerPrefix string
	WorkerName   string

	GPUOnly   bool
	CPUMining bool

	ExtraArgs []string
	LogLevel  string

	HealthEnabled bool
	HealthPort    int
	HealthBind    string

	MinerHTTPPort int
	MinerBin      string
	NvidiaSmiPath string
	DefaultsFile  string

	BackoffBase    time.Duration
	BackoffMax     time.Duration
	StartupTimeout time.Duration
	ShutdownGrace  time.Duration
}

var (
	pearlWalletRe = regexp.MustCompile(`^prl1[0-9a-z]{40,80}$`)
	genericWallet = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
	nockWalletRe  = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{20,128}$`)
	algoRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
)

// Load reads config/defaults.env (or DEFAULTS_FILE) and overlays process
// environment values. An environment variable that is present, even when
// empty, overrides the file.
func Load(lookup LookupFunc) (*Config, error) {
	defaultsPath := "/app/config/defaults.env"
	if v, ok := lookup("DEFAULTS_FILE"); ok && strings.TrimSpace(v) != "" {
		defaultsPath = strings.TrimSpace(v)
	}
	f, err := os.Open(defaultsPath)
	if err != nil {
		return nil, fmt.Errorf("config: DEFAULTS_FILE: %w", err)
	}
	defer f.Close()
	defaults, err := ParseEnvFile(f)
	if err != nil {
		return nil, fmt.Errorf("config: DEFAULTS_FILE: %w", err)
	}

	get := func(key string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return defaults[key]
	}
	getInt := func(key string, min, max int) (int, error) {
		raw := strings.TrimSpace(get(key))
		if raw == "" {
			return 0, fmt.Errorf("config: %s: is required", key)
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < min || n > max {
			return 0, fmt.Errorf("config: %s: must be an integer between %d and %d", key, min, max)
		}
		return n, nil
	}
	getBool := func(key string) (bool, error) {
		switch strings.ToLower(strings.TrimSpace(get(key))) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("config: %s: must be true or false", key)
		}
	}

	cfg := &Config{
		Algorithm:     strings.TrimSpace(get("MINING_ALGORITHM")),
		Pool:          strings.TrimSpace(get("MINING_POOL")),
		PoolFallback:  strings.TrimSpace(get("MINING_POOL_FALLBACK")),
		Wallet:        strings.TrimSpace(get("MINING_WALLET")),
		NockWallet:    strings.TrimSpace(get("NOCK_WALLET")),
		WorkerPrefix:  identity.SanitizeWorkerName(get("WORKER_PREFIX")),
		WorkerName:    identity.SanitizeWorkerName(get("WORKER_NAME")),
		LogLevel:      strings.ToLower(strings.TrimSpace(get("LOG_LEVEL"))),
		HealthBind:    strings.TrimSpace(get("HEALTH_BIND")),
		MinerBin:      strings.TrimSpace(get("BZMINER_BIN")),
		NvidiaSmiPath: strings.TrimSpace(get("NVIDIA_SMI_PATH")),
		DefaultsFile:  defaultsPath,
	}

	if cfg.Algorithm == "" {
		return nil, fmt.Errorf("config: MINING_ALGORITHM: is required")
	}
	if !algoRe.MatchString(cfg.Algorithm) {
		return nil, fmt.Errorf("config: MINING_ALGORITHM: invalid algorithm name")
	}
	if err := validatePool("MINING_POOL", cfg.Pool); err != nil {
		return nil, err
	}
	if cfg.PoolFallback != "" {
		if err := validatePool("MINING_POOL_FALLBACK", cfg.PoolFallback); err != nil {
			return nil, err
		}
	}
	if cfg.Wallet == "" {
		return nil, fmt.Errorf("config: MINING_WALLET: is required")
	}
	if strings.ContainsAny(cfg.Wallet, "+ \t") {
		return nil, fmt.Errorf("config: MINING_WALLET: must not contain whitespace or '+'")
	}
	if cfg.Algorithm == "pearl" {
		if !pearlWalletRe.MatchString(cfg.Wallet) {
			return nil, fmt.Errorf("config: MINING_WALLET: invalid Pearl address (must start with prl1)")
		}
	} else if !genericWallet.MatchString(cfg.Wallet) {
		return nil, fmt.Errorf("config: MINING_WALLET: invalid wallet for algorithm %s", cfg.Algorithm)
	}

	cfg.WalletArg = cfg.Wallet
	if cfg.NockWallet != "" {
		if cfg.Algorithm != "pearl" {
			return nil, fmt.Errorf("config: NOCK_WALLET: merge mining requires MINING_ALGORITHM=pearl")
		}
		if strings.HasPrefix(strings.ToLower(cfg.NockWallet), "0x") {
			return nil, fmt.Errorf("config: NOCK_WALLET: use a native Nockchain address, not a wrapped token")
		}
		if !nockWalletRe.MatchString(cfg.NockWallet) {
			return nil, fmt.Errorf("config: NOCK_WALLET: invalid base58 Nockchain address")
		}
		cfg.WalletArg = cfg.Wallet + "+" + cfg.NockWallet
	}

	if cfg.GPUOnly, err = getBool("GPU_ONLY"); err != nil {
		return nil, err
	}
	if cfg.CPUMining, err = getBool("CPU_MINING"); err != nil {
		return nil, err
	}
	if cfg.GPUOnly && cfg.CPUMining {
		return nil, fmt.Errorf("config: CPU_MINING: CPU_MINING=true conflicts with GPU_ONLY=true")
	}

	if cfg.WorkerPrefix == "" {
		return nil, fmt.Errorf("config: WORKER_PREFIX: must contain at least one letter or digit")
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("config: LOG_LEVEL: must be debug, info, warn, or error")
	}

	if cfg.HealthEnabled, err = getBool("HEALTH_ENABLED"); err != nil {
		return nil, err
	}
	if cfg.HealthPort, err = getInt("HEALTH_PORT", 1, 65535); err != nil {
		return nil, err
	}
	if cfg.HealthBind == "" {
		return nil, fmt.Errorf("config: HEALTH_BIND: is required")
	}
	if net.ParseIP(cfg.HealthBind) == nil {
		return nil, fmt.Errorf("config: HEALTH_BIND: must be an IP address")
	}
	if cfg.MinerHTTPPort, err = getInt("MINER_HTTP_PORT", 1, 65535); err != nil {
		return nil, err
	}
	if cfg.MinerHTTPPort == cfg.HealthPort {
		return nil, fmt.Errorf("config: MINER_HTTP_PORT: must differ from HEALTH_PORT")
	}

	base, err := getInt("RESTART_BACKOFF_BASE_SECONDS", 1, 300)
	if err != nil {
		return nil, err
	}
	max, err := getInt("RESTART_BACKOFF_MAX_SECONDS", base, 3600)
	if err != nil {
		return nil, fmt.Errorf("config: RESTART_BACKOFF_MAX_SECONDS: must be between %d and 3600", base)
	}
	startup, err := getInt("STARTUP_TIMEOUT_SECONDS", 10, 1800)
	if err != nil {
		return nil, err
	}
	grace, err := getInt("SHUTDOWN_GRACE_SECONDS", 1, 300)
	if err != nil {
		return nil, err
	}
	cfg.BackoffBase = time.Duration(base) * time.Second
	cfg.BackoffMax = time.Duration(max) * time.Second
	cfg.StartupTimeout = time.Duration(startup) * time.Second
	cfg.ShutdownGrace = time.Duration(grace) * time.Second

	rawExtra := get("MINER_EXTRA_ARGS")
	if strings.TrimSpace(rawExtra) != "" {
		if len(rawExtra) > maxExtraArgsBytes {
			return nil, fmt.Errorf("MINER_EXTRA_ARGS: too long (%d > %d bytes)", len(rawExtra), maxExtraArgsBytes)
		}
		args, err := SplitArgs(rawExtra)
		if err != nil {
			return nil, err
		}
		if err := ValidateExtraArgs(args); err != nil {
			return nil, err
		}
		cfg.ExtraArgs = args
	}

	if cfg.MinerBin == "" {
		return nil, fmt.Errorf("config: BZMINER_BIN: is required")
	}
	return cfg, nil
}

func validatePool(key, raw string) error {
	if raw == "" {
		return fmt.Errorf("config: %s: is required", key)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("config: %s: must not contain whitespace", key)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: %s: invalid URL", key)
	}
	switch u.Scheme {
	case "stratum+tcp", "stratum+ssl":
	default:
		return fmt.Errorf("config: %s: unsupported scheme %q (use stratum+tcp or stratum+ssl)", key, u.Scheme)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("config: %s: must be scheme://host:port with no path, query, or userinfo", key)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("config: %s: missing host", key)
	}
	port := u.Port()
	if port == "" {
		return fmt.Errorf("config: %s: missing port", key)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: %s: invalid port", key)
	}
	return nil
}

// CPUEnabled reports whether CPU mining is effective.
func (c *Config) CPUEnabled() bool { return c.CPUMining && !c.GPUOnly }
