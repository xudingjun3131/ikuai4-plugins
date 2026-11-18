package proxy

import (
	"crypto/md5"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func writeScope(t *testing.T, dir, body string) string {
	t.Helper()
	f := filepath.Join(dir, "scope.json")
	if err := os.WriteFile(f, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestScopeDomain(t *testing.T) {
	dir := t.TempDir()
	f := writeScope(t, dir, `{"domain_mode":"all_except","domains":["*.taobao.com","api.github.com"]}`)
	s := NewScopeStore(f, "")
	for host, want := range map[string]bool{
		"www.taobao.com:443": true,
		"taobao.com":         true,
		"api.github.com:443": true,
		"www.baidu.com":      false,
	} {
		if got := s.BypassConn(host, ""); got != want {
			t.Errorf("all_except %s: got %v want %v", host, got, want)
		}
	}
	writeScope(t, dir, `{"domain_mode":"only","domains":["api.example.com"]}`)
	s.Reload()
	if s.BypassConn("api.example.com", "") {
		t.Error("only: 列表内应解密")
	}
	if !s.BypassConn("www.baidu.com", "") {
		t.Error("only: 列表外应绕过")
	}
	writeScope(t, dir, `{"domain_mode":"only","domains":[]}`)
	s.Reload()
	if !s.BypassConn("www.baidu.com", "") {
		t.Error("only+empty: 应全绕过")
	}
}

func TestScopeDstIP(t *testing.T) {
	dir := t.TempDir()
	f := writeScope(t, dir, `{"dst_ip_mode":"all_except","dst_ips":["203.0.113.0/24","198.51.100.7"]}`)
	s := NewScopeStore(f, "")
	if !s.BypassConn("a.com", "203.0.113.55") {
		t.Error("CIDR 内应绕过")
	}
	if !s.BypassConn("a.com", "198.51.100.7") {
		t.Error("单 IP 应绕过")
	}
	if s.BypassConn("a.com", "8.8.8.8") {
		t.Error("CIDR 外不应绕过")
	}
	writeScope(t, dir, `{"dst_ip_mode":"only","dst_ips":["203.0.113.0/24"]}`)
	s.Reload()
	if s.BypassConn("a.com", "203.0.113.55") {
		t.Error("only: 列表内应解密")
	}
	if !s.BypassConn("a.com", "8.8.8.8") {
		t.Error("only: 列表外应绕过")
	}
}

func TestScopeGeo(t *testing.T) {
	dir := t.TempDir()
	geo := filepath.Join(dir, "geoip.csv")
	os.WriteFile(geo, []byte(`"203.0.113.0","203.0.113.255","US"
"114.114.114.0","114.114.114.255","CN"
`), 0644)
	f := writeScope(t, dir, `{"geo_mode":"only","countries":["CN"]}`)
	s := NewScopeStore(f, geo)
	if s.BypassConn("a.com", "114.114.114.114") {
		t.Error("only+CN: 国内 IP 应解密")
	}
	if !s.BypassConn("a.com", "203.0.113.9") {
		t.Error("only+CN: 国外 IP 应绕过")
	}
	if !s.GeoLoaded() {
		t.Error("GEOIP 应已加载")
	}
	if s.Country("203.0.113.9") != "US" {
		t.Errorf("Country(203.0.113.9)=%s want US", s.Country("203.0.113.9"))
	}
	writeScope(t, dir, `{"geo_mode":"except","countries":["CN"]}`)
	s.Reload()
	if !s.BypassConn("a.com", "114.114.114.114") {
		t.Error("except+CN: 国内应绕过")
	}
	if s.BypassConn("a.com", "203.0.113.9") {
		t.Error("except+CN: 国外不应绕过")
	}
}

func TestScopeRecord(t *testing.T) {
	dir := t.TempDir()
	f := writeScope(t, dir, `{}`)
	s := NewScopeStore(f, "")
	if !s.ShouldRecord("GET", "/api/x", "UA", "", "application/json", 100) {
		t.Error("无过滤器应全记录")
	}
	writeScope(t, dir, `{"record_mode":"only","paths":["^/api/"],"methods":["POST"],"content_types":["json"],"user_agents":["myapp"],"size_min":50,"size_max":500}`)
	s.Reload()
	if !s.ShouldRecord("POST", "/api/x", "MyApp/1.0", "", "application/json", 100) {
		t.Error("only: 全命中应记录")
	}
	if s.ShouldRecord("GET", "/static/a.js", "Chrome", "", "text/html", 10) {
		t.Error("only: 未命中不应记录")
	}
	writeScope(t, dir, `{"record_mode":"all_except","paths":["^/static/"]}`)
	s.Reload()
	if s.ShouldRecord("GET", "/static/a.js", "Chrome", "", "text/html", 10) {
		t.Error("all_except: 命中路径不应记录")
	}
	if !s.ShouldRecord("GET", "/api/x", "Chrome", "", "text/html", 10) {
		t.Error("all_except: 未命中应记录")
	}
	writeScope(t, dir, `{"record_mode":"only","ja3":["abc123"]}`)
	s.Reload()
	if !s.ShouldRecord("GET", "/", "UA", "ABC123", "", 0) {
		t.Error("JA3 大小写不敏感应命中")
	}
	if s.ShouldRecord("GET", "/", "UA", "def456", "", 0) {
		t.Error("JA3 不匹配不应记录")
	}
}

// buildClientHello 构造一个最小 ClientHello 供 JA3 测试。
func buildClientHello(version uint16, ciphers, exts []uint16, groups, formats []byte) []byte {
	body := make([]byte, 0, 128)
	body = append(body, byte(version>>8), byte(version))
	body = append(body, make([]byte, 32)...) // random
	body = append(body, 0)                   // session_id len
	body = append(body, byte(len(ciphers)*2>>8), byte(len(ciphers)*2))
	for _, c := range ciphers {
		body = append(body, byte(c>>8), byte(c))
	}
	body = append(body, byte(len(formats)))
	body = append(body, formats...)
	ext := make([]byte, 0, 32)
	for _, e := range exts {
		ext = append(ext, byte(e>>8), byte(e))
		var data []byte
		switch e {
		case 0x000a: // supported_groups: 2字节长度 + 列表
			data = append(data, byte(len(groups)*2>>8), byte(len(groups)*2))
			for _, g := range groups {
				data = append(data, g)
			}
		case 0x000b: // ec_point_formats: 1字节长度 + 列表
			data = append(data, byte(len(formats)))
			data = append(data, formats...)
		}
		ext = append(ext, byte(len(data)>>8), byte(len(data)))
		ext = append(ext, data...)
	}
	body = append(body, byte(len(ext)>>8), byte(len(ext)))
	body = append(body, ext...)
	hs := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	return append(hs, body...)
}

func TestJA3(t *testing.T) {
	hello := buildClientHello(0x0303,
		[]uint16{0x1301, 0x1302, 0x002f},
		[]uint16{0x0000, 0x000a, 0x000b, 0x0a0a}, // 含一个 GREASE 应被排除
		[]byte{0x00, 0x1d, 0x00, 0x17},           // x25519, secp256r1
		[]byte{0x00})                             // uncompressed
	got := ja3Fingerprint(hello)
	wantStr := "771,4865-4866-47,0-10-11,29-23,0"
	sum := md5.Sum([]byte(wantStr))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("ja3: got %s want %s (str=%s)", got, want, wantStr)
	}
	if ja3Fingerprint([]byte{0x16, 0x03, 0x01}) != "" {
		t.Error("非法输入应返回空")
	}
}

func TestParseCIDRs(t *testing.T) {
	nets := parseCIDRs([]string{"10.0.0.0/8", "192.168.1.5", "", "bad"})
	if len(nets) != 2 {
		t.Fatalf("got %d nets want 2", len(nets))
	}
	if !nets[1].Contains(net.ParseIP("192.168.1.5")) {
		t.Error("单 IP 应转 /32 并命中")
	}
}

func TestIpToU32(t *testing.T) {
	if got := ipToU32([]byte{1, 2, 3, 4}); got != 0x01020304 {
		t.Errorf("got %x", got)
	}
}
