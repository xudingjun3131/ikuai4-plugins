package store

import (
	"sync"
	"testing"
	"time"
)

func Test_Subscribe_receives_flow_added(t *testing.T) {
	// Given
	s := New(5)
	ch := make(chan Event, 1)
	unsub := s.Subscribe(func(ev Event) { ch <- ev })
	t.Cleanup(unsub)

	// When
	id, ok := s.Add(Flow{Request: Message{Method: "GET", URL: "http://x"}})
	if !ok {
		t.Fatal("Add failed")
	}

	// Then
	select {
	case ev := <-ch:
		if ev.Type != EventFlowAdded || ev.Flow == nil || ev.Flow.ID != id {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func Test_Subscribe_unsubscribe_stops_delivery(t *testing.T) {
	s := New(5)
	var n int
	var mu sync.Mutex
	unsub := s.Subscribe(func(Event) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	unsub()
	_, _ = s.Add(Flow{Request: Message{URL: "http://a"}})
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Fatalf("n = %d after unsub", n)
	}
}

func Test_Subscribe_cleared_and_paused(t *testing.T) {
	s := New(5)
	ch := make(chan Event, 4)
	unsub := s.Subscribe(func(ev Event) { ch <- ev })
	t.Cleanup(unsub)

	s.Clear()
	s.SetCapturePaused(true)

	want := map[string]bool{EventCleared: false, EventCapturePaused: false}
	deadline := time.After(time.Second)
	for !want[EventCleared] || !want[EventCapturePaused] {
		select {
		case <-deadline:
			t.Fatalf("got partial: %v", want)
		case ev := <-ch:
			want[ev.Type] = true
			if ev.Type == EventCapturePaused && (ev.Paused == nil || !*ev.Paused) {
				t.Fatalf("paused payload = %+v", ev.Paused)
			}
		}
	}
}
