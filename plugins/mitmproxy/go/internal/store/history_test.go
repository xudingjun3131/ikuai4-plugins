package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/store"
)

func Test_History_survives_restart(t *testing.T) {
	dir := t.TempDir()
	h, err := store.NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	s1 := store.New(100)
	f := store.Flow{
		State:     store.StateDone,
		StartedAt: time.Unix(100, 0).UTC(),
		Request:   store.Message{Method: "GET", URL: "https://hist.example/a"},
		Response:  store.Message{Status: 200},
	}
	id, ok := s1.Add(f)
	if !ok {
		t.Fatal("add")
	}
	got, _ := s1.Get(id)
	if err := h.Append(got); err != nil {
		t.Fatal(err)
	}
	// simulate restart
	h2, err := store.NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2 := store.New(100)
	n, err := h2.ReplayInto(s2)
	if err != nil || n < 1 {
		t.Fatalf("replay n=%d err=%v path=%s", n, err, filepath.Join(dir, "history.jsonl"))
	}
	list := s2.List(store.Filter{Host: "hist.example"})
	if len(list) != 1 || list[0].Request.URL != "https://hist.example/a" {
		t.Fatalf("list=%+v", list)
	}
}
