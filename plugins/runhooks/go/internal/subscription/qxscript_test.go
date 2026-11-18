package subscription

import (
	"strings"
	"testing"
)

func TestConvertQXScriptBasic(t *testing.T) {
	src := `let obj = JSON.parse($response.body);
obj.data.vip = true;
$done({body: JSON.stringify(obj)});`
	js, warns := ConvertQXScript(src, "response-body", `^https?://api\.example\.com/vip`)
	if len(warns) != 0 {
		t.Errorf("warns = %v", warns)
	}
	for _, want := range []string{
		"function onResponse(flow)",
		`var __re = new RegExp("^https?://api\\.example\\.com/vip");`,
		"if (!__re.test(flow.request.url || \"\")) return;",
		"flow.response.body = JSON.stringify(obj); return;",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("缺 %q:\n%s", want, js)
		}
	}
	if strings.Contains(js, "$done") || strings.Contains(js, "$response") {
		t.Errorf("QX 原语未清干净:\n%s", js)
	}
}

func TestConvertQXScriptSurgeDone(t *testing.T) {
	src := `$done({response: {body: "replaced"}});`
	js, _ := ConvertQXScript(src, "response-body", "")
	if !strings.Contains(js, `flow.response.body = "replaced"; return;`) {
		t.Errorf("Surge $done 嵌套形态未转换:\n%s", js)
	}
}

func TestConvertQXScriptEmptyDone(t *testing.T) {
	js, _ := ConvertQXScript(`$done({});`, "response-body", "")
	if !strings.Contains(js, "return;") {
		t.Errorf("空 $done 未转成 return:\n%s", js)
	}
	if strings.Contains(js, "$done") {
		t.Errorf("$done 残留:\n%s", js)
	}
}

func TestConvertQXScriptRequestPhase(t *testing.T) {
	js, _ := ConvertQXScript(`$request.headers["X"] = "1"; $done({});`, "request-header", "")
	if !strings.Contains(js, "function onRequest(flow)") {
		t.Errorf("request 相位未包 onRequest:\n%s", js)
	}
	if !strings.Contains(js, `flow.request.headers["X"] = "1";`) {
		t.Errorf("request headers 映射失败:\n%s", js)
	}
}

func TestConvertQXScriptUnsupportedStubs(t *testing.T) {
	src := `$notify("title", "msg");
$prefs.setValueForKey("k", "v");
let v = $prefs.valueForKey("k");
$done({});`
	js, warns := ConvertQXScript(src, "response-body", "")
	if len(warns) == 0 {
		t.Error("不支持的 API 应产生 warning")
	}
	for _, bad := range []string{"$notify", "$prefs"} {
		if strings.Contains(js, bad) {
			t.Errorf("%s 未打桩:\n%s", bad, js)
		}
	}
	for _, want := range []string{"function __noop(){}", "__prefs = {"} {
		if !strings.Contains(js, want) {
			t.Errorf("缺打桩 %q:\n%s", want, js)
		}
	}
}

func TestConvertQXScriptResidualDollar(t *testing.T) {
	js, warns := ConvertQXScript(`let x = $unknown.thing; $done({});`, "response-body", "")
	if len(warns) != 1 || !strings.Contains(warns[0], "未识别的 $ 变量") {
		t.Errorf("warns = %v", warns)
	}
	if !strings.Contains(js, "$unknown") {
		t.Errorf("未识别变量应原样保留:\n%s", js)
	}
}

func TestConvertQXScriptNoPatternNoGuard(t *testing.T) {
	js, _ := ConvertQXScript(`$done({});`, "response-body", "")
	if strings.Contains(js, "__re") {
		t.Errorf("无 pattern 不应加守卫:\n%s", js)
	}
}
