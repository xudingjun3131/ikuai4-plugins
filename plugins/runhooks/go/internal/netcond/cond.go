package netcond

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// Config is network condition simulation.
type Config struct {
	Enabled    bool          `json:"enabled"`
	Latency    time.Duration `json:"latency"`    // added delay per direction
	BandwidthB int64         `json:"bandwidthB"` // bytes/sec; 0 = unlimited
	LossPct    int           `json:"lossPct"`    // 0-100 best-effort drop (response)
}

// Cond applies throttle to proxy hook traffic.
type Cond struct {
	mu  sync.RWMutex
	cfg Config
}

func New() *Cond {
	return &Cond{}
}

func (c *Cond) Get() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

func (c *Cond) Set(cfg Config) {
	if cfg.LossPct < 0 {
		cfg.LossPct = 0
	}
	if cfg.LossPct > 100 {
		cfg.LossPct = 100
	}
	if cfg.BandwidthB < 0 {
		cfg.BandwidthB = 0
	}
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// SleepLatency blocks for configured latency when enabled.
func (c *Cond) SleepLatency() {
	cfg := c.Get()
	if !cfg.Enabled || cfg.Latency <= 0 {
		return
	}
	time.Sleep(cfg.Latency)
}

// ThrottleRead reads r applying bandwidth limit.
func (c *Cond) ThrottleRead(r io.Reader) ([]byte, error) {
	cfg := c.Get()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || cfg.BandwidthB <= 0 || len(b) == 0 {
		return b, nil
	}
	// approximate: sleep len/bandwidth
	sec := float64(len(b)) / float64(cfg.BandwidthB)
	d := time.Duration(sec * float64(time.Second))
	if d > 0 {
		time.Sleep(d)
	}
	return b, nil
}

// ShouldDrop returns true if loss simulation drops this exchange.
func (c *Cond) ShouldDrop(n int64) bool {
	cfg := c.Get()
	if !cfg.Enabled || cfg.LossPct <= 0 {
		return false
	}
	// deterministic-ish: use n modulo 100
	return int(n%100) < cfg.LossPct
}

// WrapResponse delays then returns resp (for hook use).
func (c *Cond) WrapResponse(resp *http.Response) *http.Response {
	c.SleepLatency()
	return resp
}
