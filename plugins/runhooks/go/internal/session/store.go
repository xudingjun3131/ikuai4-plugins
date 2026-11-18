package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session is a named capture partition (file-backed).
type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Active    bool      `json:"active"`
}

type fileShape struct {
	Active   string    `json:"active"`
	Sessions []Session `json:"sessions"`
}

// Store is file-backed session CRUD under dataDir/sessions.json.
type Store struct {
	mu   sync.RWMutex
	path string
	data fileShape
}

func NewStore(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "sessions.json")}
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.data.Sessions) == 0 {
		now := time.Now().UTC()
		s.data.Sessions = []Session{{ID: "default", Name: "Default", CreatedAt: now, Active: true}}
		s.data.Active = "default"
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.data)
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, len(s.data.Sessions))
	copy(out, s.data.Sessions)
	for i := range out {
		out[i].Active = out[i].ID == s.data.Active
	}
	return out
}

func (s *Store) ActiveID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Active
}

func (s *Store) Create(name string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		name = "Session"
	}
	id := fmt.Sprintf("s%d", time.Now().UnixNano())
	sess := Session{ID: id, Name: name, CreatedAt: time.Now().UTC()}
	s.data.Sessions = append(s.data.Sessions, sess)
	if err := s.saveLocked(); err != nil {
		return Session{}, err
	}
	sess.Active = id == s.data.Active
	return sess, nil
}

func (s *Store) Activate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.data.Sessions {
		if s.data.Sessions[i].ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session not found")
	}
	s.data.Active = id
	return s.saveLocked()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "default" {
		return fmt.Errorf("cannot delete default")
	}
	out := s.data.Sessions[:0]
	for _, sess := range s.data.Sessions {
		if sess.ID != id {
			out = append(out, sess)
		}
	}
	if len(out) == len(s.data.Sessions) {
		return fmt.Errorf("session not found")
	}
	s.data.Sessions = out
	if s.data.Active == id {
		s.data.Active = "default"
	}
	return s.saveLocked()
}
