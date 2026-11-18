package breakpoint_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/breakpoint"
	"github.com/GitterBois/runhooks/internal/proxy"
)

func Test_Manager_hold_and_resume_request(t *testing.T) {
	m := breakpoint.NewManager(32, time.Second)
	m.SetEnabled(true)
	m.SetMatch(breakpoint.Match{Host: "example.com"})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/a", nil)
	var got *http.Request
	var action proxy.RequestAction
	done := make(chan struct{})
	go func() {
		got, _, action = m.OnRequest(req)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	var id string
	for time.Now().Before(deadline) {
		list := m.List()
		if len(list) == 1 {
			id = list[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no session")
	}
	body := "edited"
	if err := m.Resume(id, breakpoint.ResumeEdit{
		Headers: map[string]string{"X-BP": "1"},
		Body:    &body,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnRequest hung")
	}
	if action != proxy.RequestContinue {
		t.Fatalf("action=%v", action)
	}
	if got.Header.Get("X-BP") != "1" {
		t.Fatalf("headers=%v", got.Header)
	}
	b, _ := io.ReadAll(got.Body)
	if string(b) != "edited" {
		t.Fatalf("body=%q", b)
	}
}

func Test_Manager_timeout_continues(t *testing.T) {
	m := breakpoint.NewManager(32, 40*time.Millisecond)
	m.SetEnabled(true)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/t", nil)
	start := time.Now()
	_, _, action := m.OnRequest(req)
	if action != proxy.RequestContinue {
		t.Fatalf("action=%v", action)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("returned too fast")
	}
	if len(m.List()) != 0 {
		t.Fatalf("leaked sessions: %v", m.List())
	}
}

func Test_Manager_concurrent_out_of_order_resume(t *testing.T) {
	m := breakpoint.NewManager(32, 2*time.Second)
	m.SetEnabled(true)

	type result struct {
		id  string
		hdr string
	}
	var mu sync.Mutex
	results := make([]result, 0, 2)
	var wg sync.WaitGroup

	startHold := func(path, mark string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "http://example.com"+path, nil)
			out, _, _ := m.OnRequest(req)
			mu.Lock()
			results = append(results, result{id: mark, hdr: out.Header.Get("X-Mark")})
			mu.Unlock()
		}()
	}
	startHold("/one", "one")
	startHold("/two", "two")

	// Wait for both holds
	var ids []string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		list := m.List()
		if len(list) == 2 {
			ids = []string{list[0].ID, list[1].ID}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(m.List()))
	}

	// Resume out of order: second then first
	if err := m.Resume(ids[1], breakpoint.ResumeEdit{Headers: map[string]string{"X-Mark": "second"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Resume(ids[0], breakpoint.ResumeEdit{Headers: map[string]string{"X-Mark": "first"}}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting holds")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(results) != 2 {
		t.Fatalf("results=%v", results)
	}
	// Both resumed with distinct marks (order of completion may vary)
	marks := map[string]bool{}
	for _, r := range results {
		marks[r.hdr] = true
	}
	if !marks["first"] || !marks["second"] {
		t.Fatalf("marks=%v results=%v", marks, results)
	}
}

func Test_Manager_max_skips_hold(t *testing.T) {
	m := breakpoint.NewManager(1, time.Second)
	m.SetEnabled(true)
	req1, _ := http.NewRequest(http.MethodGet, "http://example.com/1", nil)
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/2", nil)

	held := make(chan struct{})
	go func() {
		// block first
		go func() {
			for len(m.List()) == 0 {
				time.Sleep(time.Millisecond)
			}
			close(held)
		}()
		_, _, _ = m.OnRequest(req1)
	}()
	<-held
	// second should not hold (max=1)
	start := time.Now()
	_, _, action := m.OnRequest(req2)
	if action != proxy.RequestContinue {
		t.Fatalf("action=%v", action)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("second request was held despite max")
	}
	list := m.List()
	if len(list) != 1 {
		t.Fatalf("list=%d", len(list))
	}
	_ = m.Resume(list[0].ID, breakpoint.ResumeEdit{})
}

func Test_Manager_Stop_cancels(t *testing.T) {
	m := breakpoint.NewManager(32, 5*time.Second)
	m.SetEnabled(true)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/s", nil)
	done := make(chan struct{})
	go func() {
		_, _, _ = m.OnRequest(req)
		close(done)
	}()
	for len(m.List()) == 0 {
		time.Sleep(time.Millisecond)
	}
	m.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not release")
	}
}

func Test_Manager_response_phase(t *testing.T) {
	m := breakpoint.NewManager(32, time.Second)
	m.SetEnabled(true)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/r", nil)
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}
	done := make(chan *http.Response, 1)
	go func() {
		out, _ := m.OnResponse(resp)
		done <- out
	}()
	var id string
	for i := 0; i < 100; i++ {
		if list := m.List(); len(list) == 1 {
			id = list[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no session")
	}
	b := "patched"
	_ = m.Resume(id, breakpoint.ResumeEdit{Status: 201, Body: &b})
	select {
	case out := <-done:
		if out.StatusCode != 201 {
			t.Fatalf("status=%d", out.StatusCode)
		}
		rb, _ := io.ReadAll(out.Body)
		if string(rb) != "patched" {
			t.Fatalf("body=%q", rb)
		}
	case <-time.After(time.Second):
		t.Fatal("hung")
	}
}

// [match-all] match.host="*" 必须挂住任何请求。修前 hostMatch 漏了 "*" 特判,
// "*" 落到 strings.Contains(host,"*") 恒 false —— "全匹配"的断点从来没反应。
func Test_Breakpoint_matchAllHost_holds(t *testing.T) {
	m := breakpoint.NewManager(4, 300*time.Millisecond)
	m.SetEnabled(true)
	m.SetMatch(breakpoint.Match{Host: "*"})

	req, err := http.NewRequest(http.MethodGet, "http://api.example.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _, _ = m.OnRequest(req) // 命中应阻塞到超时
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("host=* 没挂住请求(全匹配失效)")
	case <-time.After(120 * time.Millisecond):
		// 仍阻塞 = 挂住了, 符合预期
	}
	sess := m.List()
	if len(sess) != 1 {
		t.Fatalf("挂起会话数=%d want 1", len(sess))
	}
	if sess[0].URL == "" {
		t.Fatal("会话没记 URL")
	}
	// 不匹配的 host 不应挂住
	m2 := breakpoint.NewManager(4, 300*time.Millisecond)
	m2.SetEnabled(true)
	m2.SetMatch(breakpoint.Match{Host: "*.nonexistent.invalid"})
	req2, _ := http.NewRequest(http.MethodGet, "http://api.example.com/y", nil)
	done2 := make(chan struct{})
	go func() {
		_, _, _ = m2.OnRequest(req2)
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("不匹配的 host 也被挂住了")
	}
}
