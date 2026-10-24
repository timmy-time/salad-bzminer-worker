package gpu

import (
	"os"
	"testing"
)

func TestParseBzMinerGPUInfo(t *testing.T) {
	data := `NVIDIA display driver: 535.230.02
CUDA driver API: ok (version 12.2)
NVIDIA CUDA devices: 2

[gpu 0] NVIDIA GeForce RTX 4090
  pci            : 0000:01:00.0
  memory         : 24.00 GiB, 384-bit
  utilization    : gpu=0%

[gpu 1] NVIDIA GeForce RTX 4090
  pci            : 0000:02:00.0
  memory         : 24564 MiB, 384-bit
`
	gpus, sawZero, err := ParseBzMinerGPUInfo(data)
	if err != nil {
		t.Fatalf("ParseBzMinerGPUInfo: %v", err)
	}
	if sawZero {
		t.Fatal("sawZero = true, want false")
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d GPUs, want 2", len(gpus))
	}
	if gpus[0].Name != "NVIDIA GeForce RTX 4090" {
		t.Errorf("name = %q", gpus[0].Name)
	}
	if gpus[0].MemoryBytes != 24*1024*1024*1024 {
		t.Errorf("GiB memory = %d", gpus[0].MemoryBytes)
	}
	if gpus[1].MemoryBytes != 24564*1024*1024 {
		t.Errorf("MiB memory = %d", gpus[1].MemoryBytes)
	}
	if gpus[1].PCIID != "0000:02:00.0" {
		t.Errorf("pci = %q", gpus[1].PCIID)
	}
}

func TestParseBzMinerGPUInfoZero(t *testing.T) {
	gpus, sawZero, err := ParseBzMinerGPUInfo("NVIDIA CUDA devices: 0\n")
	if err == nil || !sawZero || len(gpus) != 0 {
		t.Fatalf("gpus=%v sawZero=%v err=%v, want zero/true/error", gpus, sawZero, err)
	}
}

func TestParseCSV(t *testing.T) {
	data := `0, NVIDIA GeForce RTX 4090, 24564, GPU-aaaa
1, NVIDIA RTX 5090, 32768, GPU-bbbb
`
	gpus, err := parseCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 2 || gpus[0].MemoryBytes != 24564*1024*1024 || gpus[1].UUID != "GPU-bbbb" {
		t.Fatalf("unexpected parse: %+v", gpus)
	}
}

func TestParseCSVEmpty(t *testing.T) {
	if _, err := parseCSV("index, name, memory.total [MiB], uuid\n"); err == nil {
		t.Fatal("expected error for empty device list")
	}
}

func TestDetectNoGPU(t *testing.T) {
	_, err := Detect(t.Context(), Options{VisibleDevices: "none", ProcRoot: t.TempDir()})
	if err == nil {
		t.Fatal("expected no-GPU error")
	}
	if got := err.Error(); !contains(got, "no NVIDIA GPU detected") || !contains(got, `"none"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDetectNvidiaSmiScript(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/nvidia-smi"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '0, NVIDIA GeForce RTX 4090, 24564, GPU-x\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Detect(t.Context(), Options{NvidiaSmiPath: script, ProcRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "nvidia-smi" || len(res.GPUs) != 1 || res.GPUs[0].MemoryBytes != 24564*1024*1024 {
		t.Fatalf("result = %+v", res)
	}
}

func TestDetectProcFallback(t *testing.T) {
	root := t.TempDir()
	pci := root + "/0000:01:00.0"
	if err := os.MkdirAll(pci, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pci+"/information", []byte("Model: NVIDIA GeForce RTX 4090\nGPU UUID: GPU-x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gpus, err := detectProc(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 1 || gpus[0].Name != "NVIDIA GeForce RTX 4090" || gpus[0].UUID != "GPU-x" {
		t.Fatalf("gpus = %+v", gpus)
	}
}
