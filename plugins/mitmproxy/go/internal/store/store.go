package store

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Store is a thread-safe in-memory ring buffer of Flows (CaptureSink).
// When full, Add drops the oldest flow. CapturePaused skips Add only;
// Update/Get/List/Clear still work while paused.
type Store struct {
	mu     sync.RWMutex
	max    int
	order  []FlowID
	byID   map[FlowID]*Flow
	paused atomic.Bool
	seq    atomic.Uint64
	hist   *History

	subMu  sync.RWMutex
	subs   map[uint64]EventHandler
	subSeq atomic.Uint64
}

func (s *Store) SetHistory(h *History) { s.hist = h }

// New returns a Store that retains at most maxFlows entries.
// maxFlows < 1 is treated as 1.
func New(maxFlows int) *Store {
	if maxFlows < 1 {
		maxFlows = 1
	}
	return &Store{
		max:  maxFlows,
		byID: make(map[FlowID]*Flow, maxFlows),
	}
}

// Add inserts a flow. If CapturePaused, returns ("", false) without storing.
// On success assigns ID if empty, may evict the oldest when at capacity.
func (s *Store) Add(f Flow) (FlowID, bool) {
	if s.paused.Load() {
		return "", false
	}
	s.mu.Lock()
	if f.ID == "" {
		f.ID = FlowID(fmt.Sprintf("%d", s.seq.Add(1)))
	}
	if f.Tags != nil {
		f.Tags = append([]string(nil), f.Tags...)
	}
	cp := f
	s.evictLocked()
	s.byID[cp.ID] = &cp
	s.order = append(s.order, cp.ID)
	id := cp.ID
	notify := cloneFlow(&cp)
	s.mu.Unlock()
	if s.hist != nil {
		_ = s.hist.Append(notify)
	}
	s.publish(Event{Type: EventFlowAdded, Flow: &notify})
	return id, true
}

// Update applies mut to the flow with id. Returns false if missing.
// Works while capture is paused.
func (s *Store) Update(id FlowID, mut func(*Flow)) bool {
	if mut == nil {
		return false
	}
	s.mu.Lock()
	f, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	mut(f)
	if f.Tags != nil {
		f.Tags = append([]string(nil), f.Tags...)
	}
	notify := cloneFlow(f)
	s.mu.Unlock()
	s.publish(Event{Type: EventFlowUpdated, Flow: &notify})
	return true
}

// Get returns a copy of the flow, or false if missing.
func (s *Store) Get(id FlowID) (Flow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[id]
	if !ok {
		return Flow{}, false
	}
	return cloneFlow(f), true
}

// List returns matching flows oldest-first (ring order).
func (s *Store) List(filter Filter) []Flow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Flow, 0, len(s.order))
	for _, id := range s.order {
		f := s.byID[id]
		if f == nil {
			continue
		}
		if matchFilter(f, filter) {
			out = append(out, cloneFlow(f))
		}
	}
	return out
}

// Clear removes all flows.
func (s *Store) Clear() {
	s.mu.Lock()
	s.order = s.order[:0]
	clear(s.byID)
	s.mu.Unlock()
	s.publish(Event{Type: EventCleared})
}

// SetCapturePaused toggles whether Add accepts new flows.
func (s *Store) SetCapturePaused(paused bool) {
	s.paused.Store(paused)
	p := paused
	s.publish(Event{Type: EventCapturePaused, Paused: &p})
}

// CapturePaused reports whether Add is currently skipped.
func (s *Store) CapturePaused() bool {
	return s.paused.Load()
}

// Len returns the number of stored flows.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

func (s *Store) evictLocked() {
	for len(s.order) >= s.max {
		old := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, old)
	}
}

func cloneFlow(f *Flow) Flow {
	cp := *f
	if f.Tags != nil {
		cp.Tags = append([]string(nil), f.Tags...)
	}
	if f.Request.Body != nil {
		cp.Request.Body = append([]byte(nil), f.Request.Body...)
	}
	if f.Response.Body != nil {
		cp.Response.Body = append([]byte(nil), f.Response.Body...)
	}
	if f.Request.Headers != nil {
		cp.Request.Headers = cloneHeaders(f.Request.Headers)
	}
	if f.Response.Headers != nil {
		cp.Response.Headers = cloneHeaders(f.Response.Headers)
	}
	if f.WSFrames != nil {
		cp.WSFrames = cloneWSFrames(f.WSFrames)
	}
	return cp
}

func cloneWSFrames(in []WSFrame) []WSFrame {
	out := make([]WSFrame, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].Payload != nil {
			out[i].Payload = append([]byte(nil), in[i].Payload...)
		}
	}
	return out
}

func cloneHeaders(h map[string][]string) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func matchFilter(f *Flow, filter Filter) bool {
	// Legacy single fields (always AND).
	if filter.Method != "" && !strings.EqualFold(f.Request.Method, filter.Method) {
		return false
	}
	if filter.Status != 0 && f.Response.Status != filter.Status {
		return false
	}
	if filter.Host != "" {
		host := hostOfURL(f.Request.URL)
		if !strings.EqualFold(host, filter.Host) && !strings.Contains(strings.ToLower(f.Request.URL), strings.ToLower(filter.Host)) {
			return false
		}
	}
	if filter.Q != "" {
		q := strings.ToLower(filter.Q)
		hay := strings.ToLower(f.Request.Method + " " + f.Request.URL + " " + f.Error)
		if !strings.Contains(hay, q) {
			return false
		}
	}
	if len(filter.Conds) == 0 {
		return true
	}
	op := filter.Op
	if op != CondOR {
		op = CondAND
	}
	if op == CondOR {
		for i := range filter.Conds {
			if matchCond(f, filter.Conds[i]) {
				return true
			}
		}
		return false
	}
	for i := range filter.Conds {
		if !matchCond(f, filter.Conds[i]) {
			return false
		}
	}
	return true
}

func matchCond(f *Flow, c Cond) bool {
	field := strings.ToLower(strings.TrimSpace(c.Field))
	op := strings.ToLower(strings.TrimSpace(c.Op))
	val := c.Value
	if op == "" {
		switch field {
		case "method", "status", "scheme", "protocol", "tag":
			op = "eq"
		default:
			op = "contains"
		}
	}
	var actual string
	switch field {
	case "host":
		actual = hostOfURL(f.Request.URL)
	case "method":
		actual = f.Request.Method
	case "status":
		if f.Response.Status == 0 {
			actual = ""
		} else {
			actual = fmt.Sprintf("%d", f.Response.Status)
		}
	case "q", "url":
		actual = f.Request.Method + " " + f.Request.URL + " " + f.Error
	case "path":
		actual = pathOfURL(f.Request.URL)
	case "header":
		actual = headersFlat(f.Request.Headers) + " " + headersFlat(f.Response.Headers)
	case "tag":
		actual = strings.Join(f.Tags, " ")
	case "protocol":
		actual = f.Protocol
	case "scheme":
		actual = f.Scheme
	case "highlight":
		actual = f.HighlightColor + " " + f.HighlightLabel
	default:
		actual = f.Request.Method + " " + f.Request.URL + " " + f.Error
	}
	return cmpString(actual, op, val)
}

func cmpString(actual, op, want string) bool {
	switch op {
	case "eq", "=", "==":
		return strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(want))
	case "neq", "!=", "ne":
		return !strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(want))
	case "contains", "in", "":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(want))
	default:
		return strings.Contains(strings.ToLower(actual), strings.ToLower(want))
	}
}

func headersFlat(h map[string][]string) string {
	if h == nil {
		return ""
	}
	var b strings.Builder
	for k, vv := range h {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strings.Join(vv, ","))
		b.WriteByte(' ')
	}
	return b.String()
}

func pathOfURL(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[i:]
	} else {
		return "/"
	}
	if j := strings.IndexAny(s, "?#"); j >= 0 {
		s = s[:j]
	}
	return s
}

func hostOfURL(raw string) string {
	// Accept absolute or host:port forms without full URL parse dependency.
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}
