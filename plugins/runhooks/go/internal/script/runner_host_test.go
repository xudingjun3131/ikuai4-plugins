package script

import (
	"net/http"
	"testing"
)

// 复现 v1.1.16 现场: 设备启用了 add-header.js, 脚本只加一个头, 但 applyRequest
// 回写时把 req.Host 从 mail.163.com 改成 mail.163.com:443(MITM 路径 URL 带端口),
// 网易 nginx 回 400 The plain HTTP request was sent to HTTPS port。
func Test_ApplyRequest_keepsHost_whenOnlyHeadersChanged(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://mail.163.com:443/", nil)
	req.Host = "mail.163.com"
	fr, err := snapshotRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	fr.Headers["X-Proxying-Script"] = "1" // add-header.js 做的唯一改动
	applyRequest(req, fr)
	if req.Host != "mail.163.com" {
		t.Fatalf("Host 被改写成 %q(带默认端口会让 mail.163.com 回 400)", req.Host)
	}
	if got := req.Header.Get("X-Proxying-Script"); got != "1" {
		t.Errorf("脚本加的头没生效: %q", got)
	}
	if req.URL.Host != "mail.163.com:443" {
		t.Errorf("URL 不该被重建: %q", req.URL.Host)
	}
}

// 脚本真的改了 URL 且新 URL 仍带 :443 时, Host 头也必须剥掉默认端口。
func Test_ApplyRequest_stripsDefaultPort_whenURLChanged(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://mail.163.com:443/", nil)
	req.Host = "mail.163.com"
	fr, err := snapshotRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	fr.URL = "https://mail.163.com:443/js6/main.jsp"
	applyRequest(req, fr)
	if req.Host != "mail.163.com" {
		t.Errorf("Host 仍带端口: %q", req.Host)
	}
	if req.URL.Path != "/js6/main.jsp" {
		t.Errorf("URL 改动没生效: %q", req.URL.Path)
	}
}
