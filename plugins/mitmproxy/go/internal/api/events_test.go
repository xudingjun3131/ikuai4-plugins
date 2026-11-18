package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/proxying/internal/store"
)

func Test_SSE_receives_flow_added_within_2s(t *testing.T) {
	// Given: live SSE client on /api/events
	sink := store.New(10)
	srv := httptest.NewServer(NewMux(Deps{Version: "t", Store: sink}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}

	type sseLine struct {
		event string
		data  string
	}
	got := make(chan sseLine, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		var curEvent, curData string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				curData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			case line == "":
				if curEvent != "" {
					select {
					case got <- sseLine{event: curEvent, data: curData}:
					default:
					}
					curEvent, curData = "", ""
				}
			}
		}
	}()

	// When: store Add after client is connected
	time.Sleep(50 * time.Millisecond)
	id, ok := sink.Add(store.Flow{
		State:     store.StateActive,
		StartedAt: time.Now().UTC(),
		Request:   store.Message{Method: "GET", URL: "https://example.com/sse"},
	})
	if !ok {
		t.Fatal("Add failed")
	}

	// Then: flow.added within 2s
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for flow.added")
		case ev := <-got:
			if ev.event != store.EventFlowAdded {
				continue
			}
			if !strings.Contains(ev.data, string(id)) {
				t.Fatalf("data missing id %q: %s", id, ev.data)
			}
			return
		}
	}
}

func Test_SSE_method_not_allowed(t *testing.T) {
	mux := NewMux(Deps{Version: "t", Store: store.New(1)})
	req := httptest.NewRequest(http.MethodPost, "/api/events", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rr.Code)
	}
}
