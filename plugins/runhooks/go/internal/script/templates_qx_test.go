package script_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GitterBois/runhooks/internal/proxy"
	"github.com/GitterBois/runhooks/internal/script"
	"github.com/dop251/goja"
)

// 所有内置模板必须: 1) 能过 goja 解析 2) 定义 onRequest 或 onResponse
// 3) QuantumultX-Rewrite 转换来的模板必须带溯源头(源/匹配两行注释)与 URL 守卫
// 防以后手改模板改坏语法或丢了守卫说明。
func Test_BundledTemplates_allValidGoja(t *testing.T) {
	g := script.NewGallery(t.TempDir())
	list := g.List()
	if len(list) == 0 {
		t.Fatal("没有内置模板")
	}
	qxCount := 0
	for _, tp := range list {
		vm := goja.New()
		// 与 runner 一致: RunString 前先注入 flow(原版模板在顶层就用 flow)
		_ = vm.Set("flow", vm.NewObject())
		if _, err := vm.RunString(tp.Source); err != nil {
			t.Errorf("%s: goja 解析失败: %v", tp.ID, err)
			continue
		}
		hasReq := vm.Get("onRequest") != nil && !goja.IsUndefined(vm.Get("onRequest"))
		hasResp := vm.Get("onResponse") != nil && !goja.IsUndefined(vm.Get("onResponse"))
		if !hasReq && !hasResp {
			t.Errorf("%s: 既没 onRequest 也没 onResponse", tp.ID)
		}
		if strings.HasPrefix(tp.ID, "qx-") {
			qxCount++
			if !strings.Contains(tp.Source, "// 源: QuantumultX-Rewrite") {
				t.Errorf("%s: 缺溯源注释", tp.ID)
			}
			if !strings.Contains(tp.Source, "// 匹配: ") {
				t.Errorf("%s: 缺匹配条件注释", tp.ID)
			}
			// runner 对流没有匹配过滤, 模板必须自查 URL
			if !strings.Contains(tp.Source, "flow.request.url") {
				t.Errorf("%s: 没有 URL 守卫", tp.ID)
			}
		}
	}
	if qxCount < 10 {
		t.Errorf("QuantumultX-Rewrite 转换模板只有 %d 个, 预期 >= 10", qxCount)
	}
	t.Logf("共 %d 个内置模板, 其中 QX 转换 %d 个", len(list), qxCount)
}

// QX 转换模板经 runner 真实执行: 不匹配的 URL 不许改响应体(守卫生效)。
// 同时验证 Apply -> runner Reload 这条安装链路是通的。
func Test_QXTemplates_guardBlocksNonMatching(t *testing.T) {
	dir := t.TempDir()
	r, err := script.NewRunner(dir, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	g := script.NewGallery(r.Dir())
	list := g.List()
	applied := 0
	for _, tp := range list {
		if !strings.HasPrefix(tp.ID, "qx-") {
			continue
		}
		if _, err := g.Apply(tp.ID, tp.ID+".js"); err != nil {
			t.Fatalf("%s: Apply 失败: %v", tp.ID, err)
		}
		applied++
	}
	if applied == 0 {
		t.Fatal("一个 QX 模板都没装上")
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	installed := r.ListInstalled()
	if len(installed) < applied {
		t.Fatalf("runner 只加载了 %d/%d 个脚本", len(installed), applied)
	}

	// 噪音 URL: 任何 QX 模板都不该命中
	const noiseURL = "http://127.0.0.1:1/__guard_probe__"
	const body = `{"probe":"keep-me"}`
	req, err := http.NewRequest(http.MethodGet, noiseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
	out, action := r.OnResponse(resp)
	if action != proxy.ResponseContinue || out == nil {
		t.Fatalf("action=%v out=%v", action, out)
	}
	got, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(body)) {
		t.Errorf("未命中 URL 的响应体被改了:\n got: %s\nwant: %s", got, body)
	}
}
