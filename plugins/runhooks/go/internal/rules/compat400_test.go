package rules_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GitterBois/runhooks/internal/rules"
)

// 老装机 rules.json 里固化的 Enabled=true 全局改写规则, 升级后必须在加载层失效。
// seedDefaults 明确「文件已存在就不覆盖」, 只改 defaults.go 对老设备无效。
func Test_Compat400_legacy_global_rewrite_disabled(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"rules":[
		{"id":"builtin-no-cache","enabled":true,"type":"header","match":{},
		 "header":{"phase":"request","set":{"Cache-Control":"no-cache","Pragma":"no-cache"},
		           "remove":["If-None-Match","If-Modified-Since"]}},
		{"id":"builtin-accept-encoding","enabled":true,"type":"header","match":{},
		 "header":{"phase":"request","set":{"Accept-Encoding":"identity"}}}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range c.List() {
		if (r.ID == "builtin-no-cache" || r.ID == "builtin-accept-encoding") && r.Enabled {
			t.Errorf("老规则 %q 仍启用, 会改写所有请求导致部分站点 400", r.ID)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://mail.163.com/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("If-None-Match", `W/"abc"`)
	got, _, _ := c.OnRequest(req)
	if got == nil {
		t.Fatal("OnRequest 返回 nil 请求")
	}
	if v := got.Header.Get("Accept-Encoding"); v != "gzip, deflate, br, zstd" {
		t.Errorf("Accept-Encoding 被改写为 %q", v)
	}
	if v := got.Header.Get("Cache-Control"); v != "" {
		t.Errorf("Cache-Control 被注入 %q", v)
	}
	if v := got.Header.Get("If-None-Match"); v != `W/"abc"` {
		t.Errorf("If-None-Match 被删掉: %q", v)
	}
}
