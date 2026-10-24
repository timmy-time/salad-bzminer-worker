package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type metric struct {
	Value float64 `json:"value"`
}

type device struct {
	Index     int               `json:"index"`
	Name      string            `json:"name"`
	Backend   string            `json:"backend"`
	PCIID     string            `json:"pci_id"`
	Hashrate  float64           `json:"hashrate"`
	Mining    bool              `json:"mining"`
	Status    string            `json:"status"`
	Algorithm string            `json:"algorithm"`
	Extra     map[string]metric `json:"extra"`
}

type snapshot struct {
	Type    string `json:"type"`
	Seq     int    `json:"seq"`
	TSMilli int64  `json:"ts_ms"`
	Summary struct {
		TotalHashrate float64 `json:"total_hashrate"`
		AvgHashrate   float64 `json:"avg_hashrate"`
		Accepted      uint64  `json:"accepted"`
		Rejected      uint64  `json:"rejected"`
		Stale         uint64  `json:"stale"`
		Pending       uint64  `json:"pending"`
		UptimeSeconds float64 `json:"uptime_s"`
	} `json:"summary"`
	Devices []device `json:"devices"`
	Pools   []struct {
		Index     int    `json:"index"`
		URL       string `json:"url"`
		Algorithm string `json:"algorithm"`
		Connected bool   `json:"connected"`
		Accepted  uint64 `json:"accepted"`
		Rejected  uint64 `json:"rejected"`
	} `json:"pools"`
}

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-V" {
			fmt.Println("bzminer v100.50")
			return
		}
	}
	if hasArg("--gpu-info") {
		printGPUInfo()
		return
	}

	port := 4014
	httpEnabled := true
	for i, a := range os.Args[1:] {
		switch a {
		case "--http_port":
			if i+2 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+2]); err == nil {
					port = n
				}
			}
		case "--http_enabled":
			if i+2 < len(os.Args) {
				httpEnabled = os.Args[i+2] != "false"
			}
		}
	}

	if os.Getenv("FAKE_CRASH_ALWAYS") == "1" {
		fmt.Println("fake bzminer: exiting 1")
		time.Sleep(100 * time.Millisecond)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if !httpEnabled {
		fmt.Println("fake bzminer: mining without HTTP")
		<-ctx.Done()
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fakeSnapshot())
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"method":"fullstatus","bzminer_version":"v100.50"}`))
	})
	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake bzminer: listen: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("fake bzminer: listening on %s\n", srv.Addr)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if code := os.Getenv("FAKE_EXIT_CODE"); code != "" {
		n, _ := strconv.Atoi(code)
		go func() {
			time.Sleep(time.Second)
			fmt.Printf("fake bzminer: exiting %d\n", n)
			os.Exit(n)
		}()
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "fake bzminer: serve: %v\n", err)
		os.Exit(2)
	}
}

func hasArg(want string) bool {
	for _, a := range os.Args[1:] {
		if a == want {
			return true
		}
	}
	return false
}

func printGPUInfo() {
	count := 2
	if v := os.Getenv("FAKE_GPU_COUNT"); v != "" {
		count, _ = strconv.Atoi(v)
	}
	name := os.Getenv("FAKE_GPU_NAME")
	if name == "" {
		name = "NVIDIA GeForce RTX 4090"
	}
	mib := 24564
	if v := os.Getenv("FAKE_GPU_MEM_MIB"); v != "" {
		mib, _ = strconv.Atoi(v)
	}
	fmt.Printf("NVIDIA display driver: fake\nNVIDIA CUDA devices: %d\n\n", count)
	for i := range count {
		fmt.Printf("[gpu %d] %s\n", i, name)
		fmt.Printf("  pci            : 0000:0%d:00.0\n", i+1)
		fmt.Printf("  memory         : %d MiB, 256-bit\n\n", mib)
	}
}

func fakeSnapshot() snapshot {
	var s snapshot
	s.Type = "snapshot"
	s.Seq = 1
	s.TSMilli = time.Now().UnixMilli()
	s.Summary.TotalHashrate = 1234.5
	s.Summary.AvgHashrate = 1200
	s.Summary.Accepted = 7
	s.Summary.Rejected = 2
	s.Summary.UptimeSeconds = 60
	s.Devices = []device{
		{
			Index: 0, Name: "NVIDIA GeForce RTX 4090", Backend: "cuda",
			PCIID: "0000:01:00.0", Hashrate: 1234.5, Mining: true,
			Status: "Mining", Algorithm: "pearl",
			Extra: map[string]metric{"memory.dedicated.total": {Value: 24564}},
		},
	}
	s.Pools = append(s.Pools, struct {
		Index     int    `json:"index"`
		URL       string `json:"url"`
		Algorithm string `json:"algorithm"`
		Connected bool   `json:"connected"`
		Accepted  uint64 `json:"accepted"`
		Rejected  uint64 `json:"rejected"`
	}{Index: 0, URL: "stratum+tcp://fake:3360", Algorithm: "pearl", Connected: true, Accepted: 7, Rejected: 2})
	return s
}
