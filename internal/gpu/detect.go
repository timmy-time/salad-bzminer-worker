package gpu

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GPU struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	MemoryBytes uint64 `json:"memory_bytes"`
	UUID        string `json:"uuid,omitempty"`
	PCIID       string `json:"pci_id,omitempty"`
}

type Options struct {
	NvidiaSmiPath  string
	BzMinerPath    string
	VisibleDevices string
	Timeout        time.Duration
	ProcRoot       string
}

type Result struct {
	GPUs   []GPU  `json:"gpus"`
	Source string `json:"source"`
}

// Detect finds visible NVIDIA GPUs. It prefers nvidia-smi, then the miner's
// own --gpu-info output (which uses CUDA and therefore respects the runtime's
// device visibility), and finally /proc/driver/nvidia.
func Detect(ctx context.Context, opt Options) (Result, error) {
	visible := opt.VisibleDevices
	if visible == "none" || visible == "void" {
		return Result{}, noGPUError(visible, "device visibility is disabled")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 45 * time.Second
	}

	var details []string
	if path := nvidiaSmiPath(opt.NvidiaSmiPath); path != "" {
		if gpus, err := detectNvidiaSmi(ctx, path, opt.Timeout); err == nil && len(gpus) > 0 {
			return Result{GPUs: gpus, Source: "nvidia-smi"}, nil
		} else if err != nil {
			details = append(details, "nvidia-smi: "+err.Error())
		}
	}

	if opt.BzMinerPath != "" {
		gpus, sawZero, err := detectBzMiner(ctx, opt.BzMinerPath, opt.Timeout)
		if err == nil && len(gpus) > 0 {
			return Result{GPUs: gpus, Source: "bzminer"}, nil
		}
		if sawZero {
			return Result{}, noGPUError(visible, "CUDA reported zero NVIDIA devices")
		}
		if err != nil {
			details = append(details, "bzminer --gpu-info: "+err.Error())
		}
	}

	if visible == "" || visible == "all" {
		if gpus, err := detectProc(opt.ProcRoot); err == nil && len(gpus) > 0 {
			return Result{GPUs: gpus, Source: "proc"}, nil
		} else if err != nil {
			details = append(details, "/proc/driver/nvidia: "+err.Error())
		}
	}

	detail := "no CUDA devices reported"
	if len(details) > 0 {
		detail = strings.Join(details, "; ")
	}
	return Result{}, noGPUError(visible, detail)
}

func noGPUError(visible, detail string) error {
	return fmt.Errorf("no NVIDIA GPU detected (NVIDIA_VISIBLE_DEVICES=%q): %s; "+
		"expose GPUs with the container runtime (--gpus all), verify the NVIDIA driver, "+
		"and check NVIDIA_VISIBLE_DEVICES", visible, detail)
}

func nvidiaSmiPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	p, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return ""
	}
	return p
}

func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if cctx.Err() == context.DeadlineExceeded {
		return buf.Bytes(), fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return buf.Bytes(), err
	}
	return buf.Bytes(), nil
}

func parseCSV(data string) ([]GPU, error) {
	var out []GPU
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "index") {
			continue
		}
		fields := strings.SplitN(line, ",", 4)
		if len(fields) < 3 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		g := GPU{
			Index: idx,
			Name:  strings.TrimSpace(fields[1]),
		}
		if mib, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64); err == nil && mib > 0 {
			g.MemoryBytes = uint64(mib * 1024 * 1024)
		}
		if len(fields) == 4 {
			u := strings.TrimSpace(fields[3])
			if u != "[N/A]" && u != "N/A" {
				g.UUID = u
			}
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	if len(out) == 0 {
		return nil, fmt.Errorf("no devices parsed")
	}
	return out, nil
}

func detectNvidiaSmi(ctx context.Context, path string, timeout time.Duration) ([]GPU, error) {
	out, err := runCommand(ctx, timeout, path,
		"--query-gpu=index,name,memory.total,uuid", "--format=csv,noheader,nounits")
	if err != nil {
		return nil, err
	}
	return parseCSV(string(out))
}

// ParseBzMinerGPUInfo parses the CUDA section of `bzminer --gpu-info`.
// It accepts both GiB and MiB memory values.
func ParseBzMinerGPUInfo(data string) (gpus []GPU, sawZero bool, err error) {
	if strings.Contains(data, "NVIDIA CUDA devices: 0") {
		sawZero = true
	}
	var current *GPU
	flush := func() {
		if current != nil {
			gpus = append(gpus, *current)
			current = nil
		}
	}
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[gpu ") && strings.Contains(trimmed, "]") {
			flush()
			head := strings.TrimPrefix(trimmed, "[gpu ")
			closeIdx := strings.IndexByte(head, ']')
			if closeIdx < 0 {
				continue
			}
			idx, convErr := strconv.Atoi(strings.TrimSpace(head[:closeIdx]))
			if convErr != nil {
				continue
			}
			name := strings.TrimSpace(head[closeIdx+1:])
			current = &GPU{Index: idx, Name: name}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(trimmed, "pci") && strings.Contains(trimmed, ":") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "pci" && current.PCIID == "" {
				current.PCIID = strings.TrimSpace(parts[1])
			}
			continue
		}
		if strings.HasPrefix(trimmed, "memory") && current.MemoryBytes == 0 {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			value := strings.TrimSpace(parts[1])
			fields := strings.Fields(strings.SplitN(value, ",", 2)[0])
			if len(fields) >= 2 {
				if b, ok := memoryBytes(fields[0], fields[1]); ok {
					current.MemoryBytes = b
				}
			}
		}
	}
	flush()
	if len(gpus) == 0 {
		return nil, sawZero, fmt.Errorf("no CUDA devices parsed")
	}
	sort.Slice(gpus, func(i, j int) bool { return gpus[i].Index < gpus[j].Index })
	return gpus, sawZero, nil
}

func memoryBytes(value, unit string) (uint64, bool) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSuffix(unit, "B")) {
	case "gi":
		return uint64(f * 1024 * 1024 * 1024), true
	case "mi":
		return uint64(f * 1024 * 1024), true
	}
	return 0, false
}

func detectBzMiner(ctx context.Context, path string, timeout time.Duration) ([]GPU, bool, error) {
	out, err := runCommand(ctx, timeout, path, "--gpu-info")
	if err != nil {
		return nil, false, err
	}
	return ParseBzMinerGPUInfo(string(out))
}

func detectProc(root string) ([]GPU, error) {
	if root == "" {
		root = "/proc/driver/nvidia/gpus"
	}
	dirs, err := filepath.Glob(filepath.Join(root, "*", "information"))
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	var out []GPU
	for _, dir := range dirs {
		data, err := os.ReadFile(dir)
		if err != nil {
			continue
		}
		g := GPU{Index: len(out)}
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "Model":
				g.Name = strings.TrimSpace(value)
			case "GPU UUID":
				g.UUID = strings.TrimSpace(value)
			}
		}
		if g.Name != "" {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no entries")
	}
	return out, nil
}
