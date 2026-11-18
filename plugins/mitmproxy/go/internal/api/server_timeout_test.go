package api

import "testing"

// [fd-limit] 真机事故回归: WebUI 无 IdleTimeout, keep-alive 连接只增不减,
// nofile(1024) 吃光后 accept4: too many open files, WebUI 整个拒服。
// ReadTimeout/WriteTimeout 必须保持 0 —— /api/events 是 SSE 长连接。
func Test_NewServer_hasIdleTimeout(t *testing.T) {
	srv := NewServer("127.0.0.1:0", Deps{})
	if srv.IdleTimeout <= 0 {
		t.Fatalf("IdleTimeout must be > 0 to reap idle keep-alive conns, got %v", srv.IdleTimeout)
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Fatalf("ReadHeaderTimeout must be > 0, got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("ReadTimeout/WriteTimeout must stay 0 (SSE /api/events is long-lived), got read=%v write=%v",
			srv.ReadTimeout, srv.WriteTimeout)
	}
}
