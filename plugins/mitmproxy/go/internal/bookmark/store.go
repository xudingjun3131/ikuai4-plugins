package bookmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Node is a folder or leaf bookmark in a tree.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
	Folder   bool   `json:"folder"`
	Method   string `json:"method,omitempty"`
	URL      string `json:"url,omitempty"`
	Created  time.Time `json:"createdAt"`
}

type fileShape struct {
	Nodes []Node `json:"nodes"`
}

// Store is file-backed nested bookmarks.
type Store struct {
	mu   sync.RWMutex
	path string
	data fileShape
}

func NewStore(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "bookmarks.json")}
	if err := s.load(); err != nil {
		return nil, err
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

func (s *Store) List() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Node, len(s.data.Nodes))
	copy(out, s.data.Nodes)
	return out
}

func (s *Store) Add(n Node) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n.ID == "" {
		n.ID = fmt.Sprintf("b%d", time.Now().UnixNano())
	}
	if n.Name == "" {
		return Node{}, fmt.Errorf("name required")
	}
	if n.ParentID != "" {
		ok := false
		for _, p := range s.data.Nodes {
			if p.ID == n.ParentID && p.Folder {
				ok = true
				break
			}
		}
		if !ok {
			return Node{}, fmt.Errorf("parent folder not found")
		}
	}
	if n.Created.IsZero() {
		n.Created = time.Now().UTC()
	}
	s.data.Nodes = append(s.data.Nodes, n)
	if err := s.saveLocked(); err != nil {
		return Node{}, err
	}
	return n, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// remove node and descendants
	remove := map[string]bool{id: true}
	changed := true
	for changed {
		changed = false
		for _, n := range s.data.Nodes {
			if remove[n.ParentID] && !remove[n.ID] {
				remove[n.ID] = true
				changed = true
			}
		}
	}
	out := s.data.Nodes[:0]
	for _, n := range s.data.Nodes {
		if !remove[n.ID] {
			out = append(out, n)
		}
	}
	if len(out) == len(s.data.Nodes) {
		return fmt.Errorf("not found")
	}
	s.data.Nodes = out
	return s.saveLocked()
}
