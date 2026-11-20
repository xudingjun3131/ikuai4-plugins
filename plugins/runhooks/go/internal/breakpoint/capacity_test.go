package breakpoint_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/breakpoint"
)

func waitHolds(t *testing.T, m *breakpoint.Manager, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.List()) == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d holds, got %d", n, len(m.List()))
}

func newReq(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com/p", strings.NewReader("ORIGINAL-BODY"))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// v1.1.8 回归基线: 容量可观测 / 超限拒绝 / 主动 abort / 计数。
func TestManager_StatsAbortAndCapacity(t *testing.T) {
	m := breakpoint.NewManager(2, 5*time.Second)
	m.SetEnabled(true)

	done := make(chan struct{}, 3)
	spawn := func(ctx context.Context) {
		req := newReq(t, ctx) // build in the parent, not inside the goroutine
		go func() {
			m.OnRequest(req)
			done <- struct{}{}
		}()
	}
	spawn(context.Background())
	spawn(context.Background())
	waitHolds(t, m, 2)

	st := m.Stats()
	if st.Max != 2 || st.Holds != 2 || st.Total != 2 || st.TimeoutMs != 5000 {
		t.Fatalf("stats wrong: %+v", st)
	}

	// 第三个请求越过容量上限: 立即放行, rejected +1, 不新占连接。
	third := newReq(t, context.Background())
	m.OnRequest(third)
	st = m.Stats()
	if st.Rejected != 1 || st.Holds != 2 {
		t.Fatalf("capacity guard wrong: %+v", st)
	}

	// 主动 abort 一个 hold: 请求带原文立即放行, 不再 pin 到 5s 超时。
	id := m.List()[0].ID
	if err := m.Abort(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("abort did not release the held request promptly")
	}
	st = m.Stats()
	if st.Aborted != 1 || st.Holds != 1 {
		t.Fatalf("abort accounting wrong: %+v", st)
	}
	if err := m.Abort(id); err == nil {
		t.Fatal("double abort must fail")
	}
	m.Stop()
}

// v1.1.8: 客户端断开即释放 hold, 不再占满连接容量到超时。
func TestManager_ClientDisconnectReleasesHold(t *testing.T) {
	m := breakpoint.NewManager(1, 10*time.Second)
	m.SetEnabled(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := newReq(t, ctx)
	done := make(chan string, 1)
	go func() {
		m.OnRequest(req)
		b, _ := io.ReadAll(req.Body)
		done <- string(b)
	}()
	waitHolds(t, m, 1)
	cancel()
	select {
	case got := <-done:
		if got != "ORIGINAL-BODY" {
			t.Fatalf("body changed on disconnect: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client disconnect did not release the hold promptly")
	}
	st := m.Stats()
	if st.ClientGone != 1 || st.Holds != 0 {
		t.Fatalf("client-gone accounting wrong: %+v", st)
	}
}

func TestManager_SetLimits(t *testing.T) {
	m := breakpoint.NewManager(0, 0) // falls back to 32 / 60s
	if st := m.Stats(); st.Max != 32 || st.TimeoutMs != 60000 {
		t.Fatalf("defaults wrong: %+v", st)
	}
	m.SetLimits(4, 1500*time.Millisecond)
	if st := m.Stats(); st.Max != 4 || st.TimeoutMs != 1500 {
		t.Fatalf("SetLimits wrong: %+v", st)
	}
	m.SetLimits(0, 0) // invalid values are ignored, not zeroed
	if st := m.Stats(); st.Max != 4 || st.TimeoutMs != 1500 {
		t.Fatalf("invalid SetLimits clobbered limits: %+v", st)
	}
}
