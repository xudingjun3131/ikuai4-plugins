package subscription

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// newP2Manager 构造一个轻量 Manager(依赖全 nil, applyAll 对 nil 安全), 用于订阅逻辑回归。
func newP2Manager(t *testing.T, client *http.Client) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := &Manager{
		dataDir:  filepath.Join(dir, "subscriptions"),
		log:      slog.Default(),
		client:   client,
		stopCh:   make(chan struct{}),
		fetchSem: make(chan struct{}, maxConcurrentFetches),
	}
	m.subsPath = filepath.Join(m.dataDir, "subs.json")
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return m
}

// [P2-1] Add() 首拉失败必须回滚: 不残留 rules=0 坏订阅, 且同 URL 可重试(不判"已存在")。
// 现状: Add 先 append+saveLocked 再 Update, fetch 失败留下坏记录 -> 两条断言都 FAIL。
func TestAddRollbackOnFetchFailure(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// 首拉模拟源站 TLS 超时/失败
			http.Error(w, "TLS handshake timeout", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, "DOMAIN,recovered.com\n")
	}))
	defer srv.Close()

	m := newP2Manager(t, srv.Client())
	url := srv.URL + "/sub.conf"

	sub, err := m.Add(url, "flaky", FormatAuto, 0)
	if err == nil {
		t.Fatalf("首拉失败应返回 error, 却成功: %+v", sub)
	}

	// 断言 1: 失败后订阅列表不得残留坏记录
	if got := len(m.List()); got != 0 {
		t.Fatalf("FAIL: Add 首拉失败后残留 %d 条坏订阅(rules=0/last_error), 期望 0; subs=%+v", got, m.List())
	}

	// 断言 2: 源站恢复后, 同 URL 可重试成功(不被"该订阅地址已存在"挡住)
	sub2, err := m.Add(url, "flaky", FormatAuto, 0)
	if err != nil {
		t.Fatalf("FAIL: 同 URL 重试被坏记录堵住: %v", err)
	}
	if sub2.RuleCount < 1 && len(m.List()) != 1 {
		t.Fatalf("重试后订阅状态异常: sub=%+v list=%+v", sub2, m.List())
	}
}

// [P2-2] 远程引用应并发展开(有界并发), 而非逐个串行拉取。
// mock: 每个 ref 固定 delay; 串行总耗时≈N*delay, 并发应远小于。
// 现状: expandRemoteRefs 串行 for -> 计时断言 FAIL。
func TestExpandRemoteRefsConcurrent(t *testing.T) {
	const N = 12
	const delay = 80 * time.Millisecond
	var peak int32
	var inflight int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(delay)
		atomic.AddInt32(&inflight, -1)
		fmt.Fprint(w, "DOMAIN,rule.example\n")
	}))
	defer srv.Close()

	// 构造 N 个不同的远程引用
	conf := "[filter_remote]\n"
	for i := 0; i < N; i++ {
		conf += fmt.Sprintf("%s/r%d.list, enabled=true\n", srv.URL, i)
	}
	p := Parse(conf, FormatQuanX)
	if len(p.RemoteRefs) != N {
		t.Fatalf("RemoteRefs=%d want %d", len(p.RemoteRefs), N)
	}

	m := newP2Manager(t, srv.Client())
	start := time.Now()
	m.expandRemoteRefs(p, srv.URL+"/main.conf")
	elapsed := time.Since(start)

	// 断言 1: 峰值并发 > 1(证明并行拉取, 非串行)
	if peak < 2 {
		t.Fatalf("FAIL: 远程引用串行展开(峰值并发=%d, 期望>1), elapsed=%v", peak, elapsed)
	}
	// 断言 2: 总耗时显著小于串行上限 N*delay(留 0.6 余量)
	serial := time.Duration(N) * delay
	if elapsed > serial*6/10 {
		t.Fatalf("FAIL: 展开耗时 %v 未见并发收益(串行基准 %v, 峰值并发=%d)", elapsed, serial, peak)
	}
	if len(p.Rules) != N {
		t.Fatalf("rules=%d want %d", len(p.Rules), N)
	}
}

// [P2-2] 并发上限有界: 全局同时在途的 fetch 不得超过 maxConcurrentFetches, 防源站 TLS 限流。
func TestFetchConcurrencyBounded(t *testing.T) {
	var peak int32
	var inflight int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		fmt.Fprint(w, "DOMAIN,x.example\n")
	}))
	defer srv.Close()

	const total = 40
	conf := "[filter_remote]\n"
	for i := 0; i < total; i++ {
		conf += fmt.Sprintf("%s/r%d.list, enabled=true\n", srv.URL, i)
	}
	p := Parse(conf, FormatQuanX)
	m := newP2Manager(t, srv.Client())
	m.expandRemoteRefs(p, srv.URL+"/main.conf")

	if int(peak) > maxConcurrentFetches {
		t.Fatalf("FAIL: 在途 fetch 峰值 %d 超过并发上限 %d, 会触发源站限流", peak, maxConcurrentFetches)
	}
	if peak < 2 {
		t.Fatalf("FAIL: 峰值并发 %d, 未并行", peak)
	}
}
