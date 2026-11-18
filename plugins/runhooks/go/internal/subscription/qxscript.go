package subscription

import (
	"regexp"
	"strings"
)

// ConvertQXScript 把 QuanX/Surge  rewrite 脚本转换成引擎 goja 脚本。
// kind: response-body/request-body/response-header/request-header/filter(默认 response-body)。
// pattern: conf 里的 URL 匹配式(转成入口守卫; 空=不守卫)。
// 返回 (引擎JS, warnings)。转换是尽力而为: 不支持的 API 一律打桩禁用并记 warning,
// 绝不让脚本静默失效。
func ConvertQXScript(src, kind, pattern string) (string, []string) {
	var warns []string
	out := src

	// 1) 环境变量映射(顺序无关, 均为整体标识符替换)
	repl := strings.NewReplacer(
		"$response.body", "flow.response.body",
		"$response.headers", "flow.response.headers",
		"$response.status", "flow.response.status",
		"$request.body", "flow.request.body",
		"$request.headers", "flow.request.headers",
		"$request.url", "flow.request.url",
	)
	out = repl.Replace(out)

	// 2) 不支持的全局 API 打桩(先替换, 防 ReferenceError 中断整脚本)
	unsupported := []string{"$notify", "$persistentStore", "$prefs", "$task", "$crypto", "$network", "$environment", "Uint8Array"}
	for _, u := range unsupported {
		if strings.Contains(out, u) {
			warns = append(warns, "脚本使用了 "+u+"(引擎沙箱不支持), 已打桩禁用")
		}
	}
	stub := strings.NewReplacer(
		"$notify", "__noop",
		"$persistentStore", "__prefs",
		"$prefs", "__prefs",
		"$task", "__noop",
		"$crypto", "__noop",
		"$network", "__noop",
		"$environment", "__noop",
	)
	out = stub.Replace(out)

	// 3) $done({...}) → 赋值 + return
	out = convertDone(out, kind, &warns)

	// 4) 残留 $ 开头标识符检查
	if re := regexp.MustCompile(`\$[a-zA-Z]`); re.MatchString(out) {
		warns = append(warns, "脚本含未识别的 $ 变量, 可能无法完整执行")
	}

	// 5) 组装: 入口守卫 + 打桩 + 用户代码, 包进 onRequest/onResponse
	isReq := strings.HasPrefix(kind, "request")
	fnName := "onResponse"
	if isReq {
		fnName = "onRequest"
	}
	var b strings.Builder
	b.WriteString("// [runhooks] 订阅脚本自动转换(QuanX → 引擎)\n")
	if pattern != "" {
		b.WriteString("var __re = new RegExp(" + jsString(pattern) + ");\n")
		b.WriteString("if (!__re.test(flow.request.url || \"\")) return;\n")
	}
	b.WriteString("function __noop(){}\n")
	b.WriteString("var __prefs = { valueForKey: function(){ return \"\"; }, setValueForKey: function(){}, removeValueForKey: function(){} };\n")
	b.WriteString("function " + fnName + "(flow) {\n")
	b.WriteString(out)
	if !strings.HasSuffix(strings.TrimSpace(out), "}") {
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String(), warns
}

// convertDone 处理 $done(...)。
// $done({body: X}) → flow.<phase>.body = X; return;
// $done({}) / $done() → return;
func convertDone(src, kind string, warns *[]string) string {
	isReq := strings.HasPrefix(kind, "request")
	target := "flow.response"
	if isReq {
		target = "flow.request"
	}
	// 带 body 的形式(允许嵌套花括号: 用手工配平而不是正则)
	for {
		i := strings.Index(src, "$done({")
		if i < 0 {
			break
		}
		end := matchBrace(src, i+len("$done(")-1) // '(' 的位置
		if end < 0 {
			break
		}
		inner := src[i+len("$done({") : end-1] // 去掉 '})'
		bodyExpr := extractBodyExpr(inner)
		if bodyExpr == "" {
			src = src[:i] + "return;" + src[end+1:]
			continue
		}
		replacement := target + ".body = " + bodyExpr + "; return;"
		tail := end + 1
		if tail < len(src) && src[tail] == ';' {
			tail++ // 吃掉 $done(...) 后面的分号, 避免 return;;
		}
		src = src[:i] + replacement + src[tail:]
	}
	// 无参/空参形式
	src = strings.ReplaceAll(src, "$done({})", "return;")
	src = strings.ReplaceAll(src, "$done()", "return;")
	if strings.Contains(src, "$done") {
		*warns = append(*warns, "存在未能解析的 $done 调用, 已原样保留(可能报错)")
	}
	return src
}

// extractBodyExpr 从 $done 参数对象里取 body 的值(支持 $done({body:X}) 与
// Surge 的 $done({response:{body:X}}) 两种嵌套形态)。
func extractBodyExpr(inner string) string {
	for i := 0; i < len(inner); i++ {
		if inner[i] != 'b' || !strings.HasPrefix(inner[i:], "body") {
			continue
		}
		// 确认是对象键: 跳过空白后前面必须是 ',' 或 '{' 或字符串开头
		j := i - 1
		for j >= 0 && (inner[j] == ' ' || inner[j] == '	' || inner[j] == '\n' || inner[j] == '\r') {
			j--
		}
		if j >= 0 && inner[j] != ',' && inner[j] != '{' {
			continue
		}
		k := i + len("body")
		for k < len(inner) && (inner[k] == ' ' || inner[k] == '	' || inner[k] == ':') {
			k++
		}
		start := k
		d := 0
		for m := k; m < len(inner); m++ {
			switch inner[m] {
			case '{', '[', '(':
				d++
			case '}', ']', ')':
				d--
				if d < 0 {
					// 到达所属对象的右括号, 值结束
					return strings.TrimSpace(inner[start:m])
				}
			case ',':
				if d == 0 {
					return strings.TrimSpace(inner[start:m])
				}
			}
		}
		return strings.TrimSpace(inner[start:])
	}
	return ""
}

// matchBrace 从 openIdx(必须是 '(') 找配对的 ')'。
func matchBrace(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// jsString 生成 JS 字符串字面量(JSON 兼容 goja)。
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
