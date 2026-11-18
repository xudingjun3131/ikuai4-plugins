package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// 订阅层 scope.sub.json 与主配置合并: 订阅域名是白名单语义(only),
// 用户手动配置保留; 无订阅文件时行为与原来完全一致。
func TestScopeSubMerge(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "scope.json")
	sub := SubScopePath(main)
	// 用户主配置: all_except + 绕过域名
	if err := os.WriteFile(main, []byte(`{"domain_mode":"all_except","domains":["skip.com"],"paths":["/user"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewScopeStore(main, "")
	if got := s.Get(); got.DomainMode != "all_except" || !reflect.DeepEqual(got.Domains, []string{"skip.com"}) {
		t.Fatalf("无订阅层时应读主配置: %+v", got)
	}
	// 写订阅层(域名 + 路径; IP/国家由 Adapt 在域名冲突时丢弃, 这里直写模拟纯 IP 订阅另测)
	sp := Scope{Domains: []string{"a.com", "*.b.com"}, Paths: []string{"/sub"}}
	b, _ := json.Marshal(sp)
	if err := os.WriteFile(sub, b, 0o600); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	got := s.Get()
	if got.DomainMode != "only" {
		t.Errorf("订阅贡献域名后 DomainMode = %q, want only", got.DomainMode)
	}
	// 用户 only 白名单才保留; all_except 的绕过列表让位给订阅白名单
	if !reflect.DeepEqual(got.Domains, []string{"a.com", "*.b.com"}) {
		t.Errorf("Domains = %v", got.Domains)
	}
	// 记录层路径并集 + 切白名单(订阅 URL-REGEX 的意图是"记这些")
	if !reflect.DeepEqual(got.Paths, []string{"/user", "/sub"}) {
		t.Errorf("Paths = %v", got.Paths)
	}
	if got.RecordMode != "only" {
		t.Errorf("RecordMode = %q, want only", got.RecordMode)
	}
	// 行为验证: 订阅域名内 → 解密; 订阅域名外 → 绕过
	if s.BypassConn("a.com", "9.9.9.9") {
		t.Error("a.com 应解密(订阅白名单内)")
	}
	if !s.BypassConn("other.com", "9.9.9.9") {
		t.Error("other.com 应绕过(订阅白名单外)")
	}
	if s.BypassConn("x.b.com", "9.9.9.9") {
		t.Error("x.b.com 应解密(*.b.com 通配)")
	}
}

// 纯 IP 订阅(无域名规则): dst_ips 白名单生效。
func TestScopeSubMergeIPOnly(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "scope.json")
	if err := os.WriteFile(main, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := SubScopePath(main)
	if err := os.WriteFile(sub, []byte(`{"dst_ips":["1.2.3.0/24"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewScopeStore(main, "")
	got := s.Get()
	if got.DstIPMode != "only" || !reflect.DeepEqual(got.DstIPs, []string{"1.2.3.0/24"}) {
		t.Fatalf("DstIPs = %+v", got)
	}
	if s.BypassConn("any.com", "1.2.3.4") {
		t.Error("1.2.3.4 在白名单内, 应解密")
	}
	if !s.BypassConn("any.com", "9.9.9.9") {
		t.Error("9.9.9.9 不在白名单, 应绕过")
	}
}

// 用户主配置是 only 白名单时, 订阅域名与用户域名取并集。
func TestScopeSubMergeWithUserOnly(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "scope.json")
	if err := os.WriteFile(main, []byte(`{"domain_mode":"only","domains":["user.com"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := SubScopePath(main)
	if err := os.WriteFile(sub, []byte(`{"domains":["sub.com"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewScopeStore(main, "")
	got := s.Get()
	if !reflect.DeepEqual(got.Domains, []string{"user.com", "sub.com"}) {
		t.Errorf("Domains = %v, want [user.com sub.com]", got.Domains)
	}
}

// 中间通配 *kw* 匹配(DOMAIN-KEYWORD 适配)。
func TestHostMatchMiddleWildcard(t *testing.T) {
	cases := []struct {
		pattern, host string
		want          bool
	}{
		{"*keyword*", "www.keyword.com", true},
		{"*keyword*", "keyword", true},
		{"*keyword*", "other.com", false},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "example.com", true},
		{"*.example.com", "notexample.com", false},
		{"exact.com", "exact.com", true},
	}
	for _, c := range cases {
		if got := hostMatch(c.pattern, c.host); got != c.want {
			t.Errorf("hostMatch(%q, %q) = %v, want %v", c.pattern, c.host, got, c.want)
		}
	}
}
