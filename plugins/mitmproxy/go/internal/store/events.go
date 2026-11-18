package store

import "sync"

const (
	EventFlowAdded     = "flow.added"
	EventFlowUpdated   = "flow.updated"
	EventCleared       = "cleared"
	EventCapturePaused = "capture_paused"
)

// Event is a store mutation notification (SSE payload source).
type Event struct {
	Type   string
	Flow   *Flow
	Paused *bool
}

// EventHandler is called without the store lock; keep it non-blocking.
type EventHandler func(Event)

// Subscribe registers h; returned func unsubscribes (idempotent).
func (s *Store) Subscribe(h EventHandler) (unsubscribe func()) {
	if h == nil {
		return func() {}
	}
	id := s.subSeq.Add(1)
	s.subMu.Lock()
	if s.subs == nil {
		s.subs = make(map[uint64]EventHandler)
	}
	s.subs[id] = h
	s.subMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.subMu.Lock()
			delete(s.subs, id)
			s.subMu.Unlock()
		})
	}
}

func (s *Store) publish(ev Event) {
	s.subMu.RLock()
	if len(s.subs) == 0 {
		s.subMu.RUnlock()
		return
	}
	handlers := make([]EventHandler, 0, len(s.subs))
	for _, h := range s.subs {
		handlers = append(handlers, h)
	}
	s.subMu.RUnlock()
	for _, h := range handlers {
		h(ev)
	}
}
