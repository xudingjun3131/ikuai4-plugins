package ca

import (
	"testing"
	"time"
)

// 回归: 用户报「开启代理后 github.com / v2ex.com / google.com 打不开, x.com 能打开」。
// 真机实证: MITM leaf 缓存命中直接返回, 引擎连续运行超过 leafValidFor(24h) 后
// 证书 NotAfter 停留在过去(HSTS 站点的证书错误不可点「继续」, 非 HSTS 的 x.com 可绕过)。
// 这三个测试坐实「缓存无过期检查」并锁住修复后的语义。

func Test_Certificate_refreshes_expired_leaf(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	first, err := a.Certificate("github.com")
	if err != nil {
		t.Fatalf("first Certificate: %v", err)
	}
	// 模拟引擎已连续运行 24h+: 缓存里的 leaf 已过期(改 Leaf.NotAfter, 不改指针)。
	a.mu.Lock()
	first.Leaf.NotAfter = time.Now().Add(-time.Hour)
	a.mu.Unlock()

	second, err := a.Certificate("github.com")
	if err != nil {
		t.Fatalf("second Certificate: %v", err)
	}
	if !second.Leaf.NotAfter.After(time.Now()) {
		t.Fatalf("缓存返回已过期证书: NotAfter=%v (now=%v); 过期后未重签", second.Leaf.NotAfter, time.Now())
	}
	if second == first {
		t.Fatal("过期缓存未失效: 两次返回同一 *tls.Certificate")
	}
}

func Test_Certificate_refreshes_near_expiry_leaf(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	first, err := a.Certificate("v2ex.com")
	if err != nil {
		t.Fatalf("first Certificate: %v", err)
	}
	// 剩余有效期不足刷新阈值(1h)时应提前重签, 避免刚好在握手的瞬间过期。
	a.mu.Lock()
	first.Leaf.NotAfter = time.Now().Add(30 * time.Minute)
	a.mu.Unlock()

	second, err := a.Certificate("v2ex.com")
	if err != nil {
		t.Fatalf("second Certificate: %v", err)
	}
	if second == first {
		t.Fatal("临近过期的缓存未提前重签")
	}
	if !second.Leaf.NotAfter.After(time.Now().Add(23 * time.Hour)) {
		t.Fatalf("重签证书有效期异常: NotAfter=%v", second.Leaf.NotAfter)
	}
}

func Test_Certificate_keeps_fresh_cache(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	first, err := a.Certificate("google.com")
	if err != nil {
		t.Fatalf("first Certificate: %v", err)
	}
	second, err := a.Certificate("google.com")
	if err != nil {
		t.Fatalf("second Certificate: %v", err)
	}
	if second != first {
		t.Fatal("未过期的缓存被重复重签, 缓存语义被破坏")
	}
}
