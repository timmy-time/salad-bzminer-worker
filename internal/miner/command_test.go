package miner

import (
	"strings"
	"testing"

	"salad-bzminer-worker/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Algorithm:     "pearl",
		Pool:          "stratum+tcp://pearl-au.luckypool.io:3360",
		Wallet:        "prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90",
		WalletArg:     "prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90",
		MinerBin:      "/opt/bzminer/bzminer",
		GPUOnly:       true,
		LogLevel:      "info",
		HealthEnabled: true,
		MinerHTTPPort: 4014,
	}
}

func TestBuildDefault(t *testing.T) {
	argv, err := Build(testConfig(), "salad-rtx4090-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/opt/bzminer/bzminer",
		"-a", "pearl",
		"-w", "prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90",
		"-p", "stratum+tcp://pearl-au.luckypool.io:3360",
		"--worker", "salad-rtx4090-1a2b3c4d",
		"--nvidia",
		"--cpu", "0",
		"--http_enabled", "true",
		"--http_address", "127.0.0.1",
		"--http_port", "4014",
		"--log-level", "info",
		"-o", "log",
		"--no-color",
		"--disable_udp",
		"--auto-update", "off",
		"--plugin-update", "off",
		"--set", "stall_guard.enabled=false",
	}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv\n got: %#v\nwant: %#v", argv, want)
	}
}

func TestBuildNockAndFallbackAndExtra(t *testing.T) {
	cfg := testConfig()
	cfg.WalletArg += "+nock1abcdefghijkmnopqrstuvwxyz23456789"
	cfg.PoolFallback = "stratum+tcp://backup.example:3360"
	cfg.ExtraArgs = []string{"--intensity", "64"}
	argv, err := Build(cfg, "salad-rtx4090-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-w prl1px0drjsz2a9ynqc575ndcf0u7t89tkwqjefvm2v4v9yqhw8h74v6sk2fc90+nock1") {
		t.Fatalf("merge wallet missing: %v", argv)
	}
	if !strings.Contains(joined, "-p stratum+tcp://backup.example:3360") {
		t.Fatalf("fallback pool missing: %v", argv)
	}
	if argv[len(argv)-2] != "--intensity" || argv[len(argv)-1] != "64" {
		t.Fatalf("extra args not last: %v", argv)
	}
}

func TestBuildCPUEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.GPUOnly = false
	cfg.CPUMining = true
	argv, err := Build(cfg, "salad-rtx4090-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range argv {
		if a == "--cpu" {
			if i+1 < len(argv) && argv[i+1] == "0" {
				t.Fatal("--cpu 0 emitted when CPU mining is enabled")
			}
			return
		}
	}
	t.Fatal("--cpu missing")
}

func TestBuildNoDeviceIndex(t *testing.T) {
	argv, err := Build(testConfig(), "salad-rtx4090-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range argv {
		if a == "--devices" || strings.HasPrefix(a, "--devices=") {
			t.Fatalf("device selection flag emitted: %v", argv)
		}
	}
}

func TestBuildHealthDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.HealthEnabled = false
	argv, err := Build(cfg, "salad-rtx4090-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--http_enabled false") {
		t.Fatalf("expected API disabled: %v", argv)
	}
}
