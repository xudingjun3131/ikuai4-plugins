package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// History is an append-only JSONL flow log (file-backed unlimited history).
// Hot ring buffer remains in Store; History survives restart for replay.
type History struct {
	mu   sync.Mutex
	path string
}

func NewHistory(dataDir string) (*History, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return &History{path: filepath.Join(dataDir, "history.jsonl")}, nil
}

func (h *History) Path() string { return h.path }

// Append writes one flow as a JSON line.
func (h *History) Append(f Flow) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	fd, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer fd.Close()
	if _, err := fd.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// LoadAll reads all flows from the log (oldest first). Bad lines are skipped.
func (h *History) LoadAll() ([]Flow, error) {
	if h == nil {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fd, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer fd.Close()
	var out []Flow
	sc := bufio.NewScanner(fd)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var f Flow
		if err := json.Unmarshal(line, &f); err != nil {
			continue
		}
		out = append(out, f)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("history scan: %w", err)
	}
	return out, nil
}

// ReplayInto loads history into store (honors max capacity via Add eviction).
func (h *History) ReplayInto(s *Store) (int, error) {
	flows, err := h.LoadAll()
	if err != nil {
		return 0, err
	}
	n := 0
	wasPaused := s.CapturePaused()
	s.SetCapturePaused(false)
	prev := s.hist
	s.hist = nil
	for _, f := range flows {
		if _, ok := s.Add(f); ok {
			n++
		}
	}
	s.hist = prev
	if wasPaused {
		s.SetCapturePaused(true)
	}
	return n, nil
}
