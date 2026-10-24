package bzapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxBody = 4 << 20

type Metric struct {
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Category string  `json:"category"`
}

type Summary struct {
	TotalHashrate float64  `json:"total_hashrate"`
	AvgHashrate   float64  `json:"avg_hashrate"`
	Accepted      uint64   `json:"accepted"`
	Rejected      uint64   `json:"rejected"`
	Stale         uint64   `json:"stale"`
	Pending       uint64   `json:"pending"`
	UptimeSeconds float64  `json:"uptime_s"`
	PowerW        *float64 `json:"power_w"`
}

type Device struct {
	Index        int               `json:"index"`
	Name         string            `json:"name"`
	Backend      string            `json:"backend"`
	PCIID        string            `json:"pci_id"`
	Hashrate     float64           `json:"hashrate"`
	AvgHashrate  float64           `json:"avg_hashrate"`
	PoolHashrate float64           `json:"pool_hashrate"`
	Accepted     uint64            `json:"accepted"`
	Rejected     uint64            `json:"rejected"`
	Stale        uint64            `json:"stale"`
	Pending      uint64            `json:"pending"`
	Errors       uint64            `json:"errors"`
	TempC        *float64          `json:"temp_c"`
	HotspotC     *float64          `json:"hotspot_c"`
	PowerW       *float64          `json:"power_w"`
	FanPct       *float64          `json:"fan_pct"`
	CoreMHz      *float64          `json:"core_mhz"`
	MemMHz       *float64          `json:"mem_mhz"`
	UtilPct      *float64          `json:"util_pct"`
	Mining       bool              `json:"mining"`
	Status       string            `json:"status"`
	Pool         int               `json:"pool"`
	Algorithm    string            `json:"algorithm"`
	Worker       string            `json:"worker"`
	Parent       int               `json:"parent"`
	Extra        map[string]Metric `json:"extra"`
}

type Pool struct {
	Index        int     `json:"index"`
	URL          string  `json:"url"`
	User         string  `json:"user"`
	Algorithm    string  `json:"algorithm"`
	Connected    bool    `json:"connected"`
	Difficulty   float64 `json:"difficulty"`
	LatencyMS    *int64  `json:"latency_ms"`
	Accepted     uint64  `json:"accepted"`
	Rejected     uint64  `json:"rejected"`
	Stale        uint64  `json:"stale"`
	Pending      uint64  `json:"pending"`
	PoolHashrate float64 `json:"pool_hashrate"`
	Height       uint64  `json:"height"`
	ShareWork    float64 `json:"share_work"`
	Reconnects   uint64  `json:"reconnects"`
	Hashrate     float64 `json:"hashrate"`
	AvgHashrate  float64 `json:"avg_hashrate"`
}

type Snapshot struct {
	Type      string    `json:"type"`
	Seq       int64     `json:"seq"`
	TSMillis  int64     `json:"ts_ms"`
	Summary   Summary   `json:"summary"`
	Devices   []Device  `json:"devices"`
	Pools     []Pool    `json:"pools"`
	FetchedAt time.Time `json:"-"`
}

func (s *Snapshot) ConnectedPool() bool {
	for _, p := range s.Pools {
		if p.Connected {
			return true
		}
	}
	return false
}

type Client struct {
	BaseURL  string
	HTTP     *http.Client
	Interval time.Duration
}

func New(baseURL string, interval time.Duration) *Client {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Client{
		BaseURL:  baseURL,
		HTTP:     &http.Client{Timeout: 2 * time.Second},
		Interval: interval,
	}
}

func (c *Client) Fetch(ctx context.Context) (*Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/snapshot", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bzminer API: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, fmt.Errorf("bzminer API: decode: %w", err)
	}
	if snap.Type != "snapshot" {
		return nil, fmt.Errorf("bzminer API: unexpected type %q", snap.Type)
	}
	snap.FetchedAt = time.Now()
	return &snap, nil
}
