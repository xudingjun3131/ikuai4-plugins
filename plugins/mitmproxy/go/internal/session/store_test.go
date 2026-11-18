package session_test

import (
	"path/filepath"
	"testing"

	"github.com/GitterBois/proxying/internal/session"
)

func Test_Sessions_isolated_active(t *testing.T) {
	dir := t.TempDir()
	s, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID() != "default" {
		t.Fatalf("active=%q", s.ActiveID())
	}
	a, err := s.Create("A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("B")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("ids collide")
	}
	if err := s.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	if s.ActiveID() != b.ID {
		t.Fatalf("active=%q want %q", s.ActiveID(), b.ID)
	}
	// reload
	s2, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.ActiveID() != b.ID {
		t.Fatalf("persist active=%q", s2.ActiveID())
	}
	if n := len(s2.List()); n != 3 {
		t.Fatalf("list=%d file=%s", n, filepath.Join(dir, "sessions.json"))
	}
}
