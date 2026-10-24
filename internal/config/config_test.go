package config

import (
	"os"
	"strings"
	"testing"
)

const testDefaults = `MINING_ALGORITHM=pearl
MINING_POOL=stratum+tcp://pearl-au.luckypool.io:3360
MINING_WALLET=prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90
WORKER_PREFIX=salad
WORKER_NAME=
GPU_ONLY=true
CPU_MINING=false
NOCK_WALLET=
MINER_EXTRA_ARGS=
LOG_LEVEL=info
HEALTH_ENABLED=true
HEALTH_PORT=8080
HEALTH_BIND=0.0.0.0
MINER_HTTP_PORT=4014
BZMINER_BIN=/app/bzminer
RESTART_BACKOFF_BASE_SECONDS=2
RESTART_BACKOFF_MAX_SECONDS=300
STARTUP_TIMEOUT_SECONDS=180
SHUTDOWN_GRACE_SECONDS=20
`

func loadTest(t *testing.T, overrides map[string]string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/defaults.env"
	if err := writeFile(path, testDefaults); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"DEFAULTS_FILE": path}
	for k, v := range overrides {
		env[k] = v
	}
	return Load(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestDefaults(t *testing.T) {
	cfg, err := loadTest(t, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Algorithm != "pearl" {
		t.Errorf("algorithm = %q", cfg.Algorithm)
	}
	if cfg.Pool != "stratum+tcp://pearl-au.luckypool.io:3360" {
		t.Errorf("pool = %q", cfg.Pool)
	}
	if cfg.Wallet != "prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90" {
		t.Errorf("wallet = %q", cfg.Wallet)
	}
	if cfg.WalletArg != cfg.Wallet {
		t.Errorf("wallet arg = %q", cfg.WalletArg)
	}
	if cfg.WorkerPrefix != "salad" || !cfg.GPUOnly || cfg.CPUMining {
		t.Errorf("worker/gpu defaults wrong: %+v", cfg)
	}
	if !cfg.HealthEnabled || cfg.HealthPort != 8080 || cfg.HealthBind != "0.0.0.0" {
		t.Errorf("health defaults wrong: %+v", cfg)
	}
	if cfg.MinerHTTPPort != 4014 || cfg.BackoffBase.Seconds() != 2 || cfg.ShutdownGrace.Seconds() != 20 {
		t.Errorf("runtime defaults wrong: %+v", cfg)
	}
	if cfg.CPUEnabled() {
		t.Error("CPUEnabled() = true with default GPU-only config")
	}
}

func TestWalletOverride(t *testing.T) {
	wallet := "prl1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
	cfg, err := loadTest(t, map[string]string{"MINING_WALLET": wallet})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wallet != wallet || cfg.WalletArg != wallet {
		t.Fatalf("wallet override failed: %+v", cfg)
	}
}

func TestInvalidWallet(t *testing.T) {
	for _, wallet := range []string{
		"bad",
		"prl1short",
		"prl1" + strings.Repeat("!", 60),
		"prl1" + strings.Repeat("a", 40) + " x",
		"prl1" + strings.Repeat("a", 40) + "+x",
	} {
		if _, err := loadTest(t, map[string]string{"MINING_WALLET": wallet}); err == nil {
			t.Errorf("wallet %q accepted", wallet)
		}
	}
}

func TestNockMerge(t *testing.T) {
	nock := "nock1abcdefghijkmnopqrstuvwxyz23456789"
	cfg, err := loadTest(t, map[string]string{"NOCK_WALLET": nock})
	if err != nil {
		t.Fatal(err)
	}
	want := cfg.Wallet + "+" + nock
	if cfg.WalletArg != want {
		t.Fatalf("wallet arg = %q, want %q", cfg.WalletArg, want)
	}
}

func TestNockRejectsWrappedAndNonPearl(t *testing.T) {
	if _, err := loadTest(t, map[string]string{"NOCK_WALLET": "0xdeadbeef"}); err == nil {
		t.Fatal("0x NOCK address accepted")
	}
	if _, err := loadTest(t, map[string]string{
		"MINING_ALGORITHM": "ergo",
		"MINING_WALLET":    "9" + strings.Repeat("A", 50),
		"NOCK_WALLET":      "nock1abcdefghijkmnopqrstuvwxyz23456789",
	}); err == nil {
		t.Fatal("NOCK accepted for non-pearl algorithm")
	}
}

func TestCPUMiningRules(t *testing.T) {
	cfg, err := loadTest(t, map[string]string{"GPU_ONLY": "false", "CPU_MINING": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CPUEnabled() {
		t.Fatal("CPUEnabled() = false")
	}
	if _, err := loadTest(t, map[string]string{"GPU_ONLY": "true", "CPU_MINING": "true"}); err == nil {
		t.Fatal("conflicting CPU/GPU flags accepted")
	}
	if _, err := loadTest(t, map[string]string{"GPU_ONLY": "maybe"}); err == nil {
		t.Fatal("invalid bool accepted")
	}
}

func TestPoolValidation(t *testing.T) {
	for _, pool := range []string{
		"http://pool:3333",
		"stratum+tcp://pool",
		"stratum+tcp://pool:99999",
		"stratum+tcp://user:pass@pool:3333",
		"stratum+tcp://pool:3333/path",
		"stratum+tcp://pool:3333?x=1",
	} {
		if _, err := loadTest(t, map[string]string{"MINING_POOL": pool}); err == nil {
			t.Errorf("pool %q accepted", pool)
		}
	}
}

func TestPortCollision(t *testing.T) {
	if _, err := loadTest(t, map[string]string{"HEALTH_PORT": "4014"}); err == nil {
		t.Fatal("colliding ports accepted")
	}
}

func TestExtraArgsValidation(t *testing.T) {
	cfg, err := loadTest(t, map[string]string{
		"MINER_EXTRA_ARGS": `--intensity 64 --oc-fan-speed "t:70[30-90]" literal\ space`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--intensity", "64", "--oc-fan-speed", "t:70[30-90]", "literal space"}
	if len(cfg.ExtraArgs) != len(want) {
		t.Fatalf("args = %#v", cfg.ExtraArgs)
	}
	for i := range want {
		if cfg.ExtraArgs[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, cfg.ExtraArgs[i], want[i])
		}
	}
}

func TestExtraArgsManagedFlags(t *testing.T) {
	for _, raw := range []string{
		"--http_port=9999",
		"--set wallet=bad",
		"--wallet prl1x",
		"--cpu 4",
		"--cpu2 0",
		"--nvidia 0",
		"--devices 0",
		"--output tui",
		"--config /tmp/x",
	} {
		if _, err := loadTest(t, map[string]string{"MINER_EXTRA_ARGS": raw}); err == nil {
			t.Errorf("extra args %q accepted", raw)
		}
	}
	if _, err := loadTest(t, map[string]string{
		"MINER_EXTRA_ARGS": "--cpu_threads 4 --cpu_affinity 0-3",
	}); err != nil {
		t.Fatalf("CPU tuning flags rejected: %v", err)
	}
}

func TestExtraArgsInjectionStaysLiteral(t *testing.T) {
	args, err := SplitArgs(`"; rm -rf /" $(reboot) $HOME`)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[0] != "; rm -rf /" || args[1] != "$(reboot)" || args[2] != "$HOME" {
		t.Fatalf("args = %#v", args)
	}
	if err := ValidateExtraArgs(args); err != nil {
		t.Fatalf("literal args rejected: %v", err)
	}
}

func TestExtraArgsQuotesAndControl(t *testing.T) {
	if _, err := SplitArgs(`--x "unterminated`); err == nil {
		t.Fatal("unmatched double quote accepted")
	}
	if _, err := SplitArgs(`--x 'unterminated`); err == nil {
		t.Fatal("unmatched single quote accepted")
	}
	if _, err := SplitArgs("--x\n--y"); err == nil {
		t.Fatal("newline accepted")
	}
	if _, err := SplitArgs("--x \"a\nb\""); err == nil {
		t.Fatal("newline in quoted value accepted")
	}
}

func TestWorkerNameAndPrefix(t *testing.T) {
	cfg, err := loadTest(t, map[string]string{"WORKER_NAME": "My Rig!", "WORKER_PREFIX": "My Farm"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkerName != "my-rig" || cfg.WorkerPrefix != "my-farm" {
		t.Fatalf("names not sanitized: %+v", cfg)
	}
	if _, err := loadTest(t, map[string]string{"WORKER_PREFIX": "!!!"}); err == nil {
		t.Fatal("empty sanitized prefix accepted")
	}
}

func TestBackoffValidation(t *testing.T) {
	if _, err := loadTest(t, map[string]string{"RESTART_BACKOFF_MAX_SECONDS": "1"}); err == nil {
		t.Fatal("max < base accepted")
	}
	if _, err := loadTest(t, map[string]string{"STARTUP_TIMEOUT_SECONDS": "5"}); err == nil {
		t.Fatal("startup timeout below minimum accepted")
	}
}
