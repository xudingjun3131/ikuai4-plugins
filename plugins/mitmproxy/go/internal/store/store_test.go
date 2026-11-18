package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func Test_Store_Add_assigns_id_and_Get_returns_copy(t *testing.T) {
	// Given
	s := New(10)
	in := Flow{
		State:     StateActive,
		StartedAt: time.Unix(1, 0).UTC(),
		Scheme:    "https",
		Request: Message{
			Method: "GET",
			URL:    "https://example.com/a",
			Proto:  "HTTP/1.1",
			Body:   []byte("req"),
		},
		Tags: []string{"t1"},
	}

	// When
	id, ok := s.Add(in)

	// Then
	if !ok || id == "" {
		t.Fatalf("Add = (%q, %v), want non-empty id and true", id, ok)
	}
	got, found := s.Get(id)
	if !found {
		t.Fatal("Get: not found")
	}
	if got.ID != id || got.Request.Method != "GET" || string(got.Request.Body) != "req" {
		t.Fatalf("Get = %+v", got)
	}
	// Mutating returned body must not affect store
	got.Request.Body[0] = 'X'
	again, _ := s.Get(id)
	if string(again.Request.Body) != "req" {
		t.Fatalf("store body mutated via Get copy: %q", again.Request.Body)
	}
}

func Test_Store_Add_evicts_oldest_when_full(t *testing.T) {
	// Given
	s := New(3)
	var ids []FlowID
	for i := 0; i < 3; i++ {
		id, ok := s.Add(Flow{Request: Message{Method: "GET", URL: fmt.Sprintf("http://h/%d", i)}})
		if !ok {
			t.Fatalf("Add %d failed", i)
		}
		ids = append(ids, id)
	}

	// When — 4th forces drop of oldest
	id4, ok := s.Add(Flow{Request: Message{Method: "POST", URL: "http://h/3"}})
	if !ok {
		t.Fatal("Add 4th failed")
	}

	// Then
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	if _, found := s.Get(ids[0]); found {
		t.Fatalf("oldest %q should be evicted", ids[0])
	}
	if _, found := s.Get(ids[1]); !found {
		t.Fatalf("id %q should remain", ids[1])
	}
	if _, found := s.Get(id4); !found {
		t.Fatal("newest missing")
	}
	list := s.List(Filter{})
	if len(list) != 3 || list[0].ID != ids[1] || list[2].ID != id4 {
		t.Fatalf("List order = %v", idsOf(list))
	}
}

func Test_Store_CapturePaused_skips_Add_only(t *testing.T) {
	// Given
	s := New(5)
	id, ok := s.Add(Flow{Request: Message{Method: "GET", URL: "http://a"}})
	if !ok {
		t.Fatal("seed Add failed")
	}

	// When
	s.SetCapturePaused(true)
	if !s.CapturePaused() {
		t.Fatal("CapturePaused want true")
	}
	_, added := s.Add(Flow{Request: Message{Method: "POST", URL: "http://b"}})

	// Then — Add skipped
	if added {
		t.Fatal("Add while paused should return false")
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}

	// Update still works while paused
	updated := s.Update(id, func(f *Flow) {
		f.State = StateDone
		f.Response.Status = 204
	})
	if !updated {
		t.Fatal("Update while paused should succeed")
	}
	got, _ := s.Get(id)
	if got.State != StateDone || got.Response.Status != 204 {
		t.Fatalf("Update effect missing: %+v", got)
	}

	// Resume accepts Add
	s.SetCapturePaused(false)
	_, ok = s.Add(Flow{Request: Message{Method: "HEAD", URL: "http://c"}})
	if !ok || s.Len() != 2 {
		t.Fatalf("resume Add failed: ok=%v len=%d", ok, s.Len())
	}
}

func Test_Store_Clear_empties(t *testing.T) {
	s := New(5)
	s.Add(Flow{Request: Message{URL: "http://x"}})
	s.Add(Flow{Request: Message{URL: "http://y"}})
	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Len after Clear = %d", s.Len())
	}
	if list := s.List(Filter{}); len(list) != 0 {
		t.Fatalf("List after Clear = %d", len(list))
	}
}

func Test_Store_List_filter_method_status_host_q(t *testing.T) {
	s := New(10)
	s.Add(Flow{Request: Message{Method: "GET", URL: "https://api.example.com/v1"}, Response: Message{Status: 200}})
	s.Add(Flow{Request: Message{Method: "POST", URL: "https://api.example.com/v1"}, Response: Message{Status: 201}})
	s.Add(Flow{Request: Message{Method: "GET", URL: "https://other.test/x"}, Response: Message{Status: 404}, Error: "not found"})

	if n := len(s.List(Filter{Method: "GET"})); n != 2 {
		t.Fatalf("Method GET: %d", n)
	}
	if n := len(s.List(Filter{Status: 201})); n != 1 {
		t.Fatalf("Status 201: %d", n)
	}
	if n := len(s.List(Filter{Host: "api.example.com"})); n != 2 {
		t.Fatalf("Host api: %d", n)
	}
	if n := len(s.List(Filter{Q: "not found"})); n != 1 {
		t.Fatalf("Q error text: %d", n)
	}
}

func Test_Store_List_multi_cond_and_or_highlight(t *testing.T) {
	s := New(10)
	s.Add(Flow{
		Request:        Message{Method: "GET", URL: "https://api.example.com/users"},
		Response:       Message{Status: 200},
		HighlightColor: "red",
		HighlightLabel: "api",
	})
	s.Add(Flow{
		Request:  Message{Method: "POST", URL: "https://api.example.com/users"},
		Response: Message{Status: 201},
	})
	s.Add(Flow{
		Request:  Message{Method: "GET", URL: "https://cdn.example.com/x"},
		Response: Message{Status: 200},
	})

	and3 := Filter{
		Op: CondAND,
		Conds: []Cond{
			{Field: "host", Op: "contains", Value: "api.example.com"},
			{Field: "method", Op: "eq", Value: "GET"},
			{Field: "status", Op: "eq", Value: "200"},
		},
	}
	got := s.List(and3)
	if len(got) != 1 || got[0].HighlightColor != "red" {
		t.Fatalf("AND 3-cond: %+v", got)
	}

	orFilter := Filter{
		Op: CondOR,
		Conds: []Cond{
			{Field: "method", Op: "eq", Value: "POST"},
			{Field: "highlight", Op: "contains", Value: "red"},
		},
	}
	if n := len(s.List(orFilter)); n != 2 {
		t.Fatalf("OR cond: %d", n)
	}
}

func Test_Store_concurrent_Add_no_race_and_cap(t *testing.T) {
	const (
		max  = 50
		nGor = 8
		nPer = 200
	)
	s := New(max)
	var wg sync.WaitGroup
	wg.Add(nGor)
	for g := 0; g < nGor; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < nPer; i++ {
				s.Add(Flow{
					Request: Message{
						Method: "GET",
						URL:    fmt.Sprintf("http://g%d/%d", g, i),
					},
				})
			}
		}(g)
	}
	wg.Wait()

	if s.Len() > max {
		t.Fatalf("Len = %d > max %d", s.Len(), max)
	}
	if s.Len() != max {
		t.Fatalf("Len = %d, want full cap %d after many Adds", s.Len(), max)
	}
	// Concurrent Get/List while more Adds
	wg.Add(nGor)
	for g := 0; g < nGor; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.List(Filter{})
				s.Add(Flow{Request: Message{URL: fmt.Sprintf("http://late/%d/%d", g, i)}})
				if id, ok := s.Add(Flow{Request: Message{URL: "http://u"}}); ok {
					s.Update(id, func(f *Flow) { f.State = StateDone })
					_, _ = s.Get(id)
				}
			}
		}(g)
	}
	wg.Wait()
	if s.Len() > max {
		t.Fatalf("after mixed ops Len = %d > max", s.Len())
	}
}

func Test_Store_Update_missing_returns_false(t *testing.T) {
	s := New(2)
	if s.Update(FlowID("nope"), func(f *Flow) { f.State = StateError }) {
		t.Fatal("Update missing should be false")
	}
}

func idsOf(flows []Flow) []FlowID {
	out := make([]FlowID, len(flows))
	for i, f := range flows {
		out[i] = f.ID
	}
	return out
}
