package core

import (
	"sync"
	"time"
)

// Audit is a bounded in-memory ring of behavior events. Strength borrowed from
// nlbwmon (per-device accounting) + Sniffnet (traffic-timeline UI): every
// decision carries client, app, category, action and timestamp, so the UI can
// answer "who visited what, when, and was it blocked".
//
// Persistence is intentionally delegated to service.sh (append to a JSONL file
// on the storage volume). Keeping the ring in memory avoids unbounded flash
// writes on a router — a real constraint on iKuai devices.
type Audit struct {
	mu   sync.RWMutex
	buf  []Decision
	cap  int
	head int
	n    int
}

func NewAudit(capacity int) *Audit {
	if capacity < 1 {
		capacity = 2000
	}
	return &Audit{buf: make([]Decision, capacity), cap: capacity}
}

func (a *Audit) Record(d Decision) {
	a.mu.Lock()
	a.buf[a.head] = d
	a.head = (a.head + 1) % a.cap
	if a.n < a.cap {
		a.n++
	}
	a.mu.Unlock()
}

type AuditQuery struct {
	ClientIP string
	App      string
	Category Category
	Action   Action
	Since    time.Time
	Limit    int
}

func (a *Audit) Query(q AuditQuery) []Decision {
	a.mu.RLock()
	defer a.mu.RUnlock()
	limit := q.Limit
	if limit <= 0 || limit > a.n {
		limit = a.n
	}
	out := make([]Decision, 0, limit)
	// newest first
	for k := 0; k < a.n && len(out) < limit; k++ {
		idx := (a.head - 1 - k + a.cap) % a.cap
		d := a.buf[idx]
		if q.ClientIP != "" && d.ClientIP != q.ClientIP {
			continue
		}
		if q.App != "" && d.App != q.App {
			continue
		}
		if q.Category != "" && d.Category != q.Category {
			continue
		}
		if q.Action != "" && d.Action != q.Action {
			continue
		}
		if !q.Since.IsZero() && d.At.Before(q.Since) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// Summary aggregates the recent window for the dashboard KPI cards.
type Summary struct {
	Total   int            `json:"total"`
	Blocked int            `json:"blocked"`
	ByApp   map[string]int `json:"by_app"`
	ByCat   map[string]int `json:"by_cat"`
	Clients int            `json:"clients"`
}

func (a *Audit) Summary(since time.Time) Summary {
	s := Summary{ByApp: map[string]int{}, ByCat: map[string]int{}}
	clients := map[string]bool{}
	for _, d := range a.Query(AuditQuery{Since: since, Limit: 1 << 30}) {
		s.Total++
		if d.Blocked() {
			s.Blocked++
		}
		if d.App != "" {
			s.ByApp[d.App]++
		}
		s.ByCat[string(d.Category)]++
		clients[d.ClientIP] = true
	}
	s.Clients = len(clients)
	return s
}
