package proxy

import (
	"net/http"
	"testing"
)

func reqWith(h map[string]string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return r
}

func Test_clientIPFromRequest(t *testing.T) {
	cases := []struct {
		name string
		h    map[string]string
		want string
	}{
		{"无头回退空", map[string]string{}, ""},
		{"XFF 单值", map[string]string{"X-Forwarded-For": "10.10.10.5"}, "10.10.10.5"},
		{"XFF 多值取最左", map[string]string{"X-Forwarded-For": "10.10.10.5, 10.10.10.1, 192.168.9.1"}, "10.10.10.5"},
		{"XFF 跳过非法再取", map[string]string{"X-Forwarded-For": "not-an-ip, 10.10.10.5"}, "10.10.10.5"},
		{"XFF 带端口", map[string]string{"X-Forwarded-For": "10.10.10.5:54321"}, "10.10.10.5"},
		{"X-Real-IP", map[string]string{"X-Real-IP": "10.10.10.5"}, "10.10.10.5"},
		{"X-Client-IP", map[string]string{"X-Client-IP": "10.10.10.5"}, "10.10.10.5"},
		{"CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "10.10.10.5"}, "10.10.10.5"},
		{"RFC7239 Forwarded", map[string]string{"Forwarded": `for=10.10.10.5;proto=https`}, "10.10.10.5"},
		{"Forwarded IPv6 带端口", map[string]string{"Forwarded": `for="[2001:db8::1]:1234"`}, "2001:db8::1"},
		{"XFF 优先于 X-Real-IP", map[string]string{"X-Forwarded-For": "10.10.10.5", "X-Real-IP": "10.10.10.9"}, "10.10.10.5"},
		{"全非法回退空", map[string]string{"X-Forwarded-For": "garbage", "X-Real-IP": "also-garbage"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clientIPFromRequest(reqWith(c.h)); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func Test_clientAddrOf_fallback_and_override(t *testing.T) {
	r := reqWith(map[string]string{})
	r.RemoteAddr = "10.10.10.1:34098"
	if got := clientAddrOf(r); got != "10.10.10.1:34098" {
		t.Errorf("无头应回退 TCP 对端, got %q", got)
	}
	r.Header.Set("X-Forwarded-For", "10.10.10.5")
	if got := clientAddrOf(r); got != "10.10.10.5" {
		t.Errorf("有 XFF 应显示真实客户端, got %q", got)
	}
}
