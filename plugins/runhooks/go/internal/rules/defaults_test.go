package rules_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/GitterBois/runhooks/internal/rules"
)

// 内置规则集必须自身合法: 类型已知、路径正则可编译、body_replace 非空、
// header 阶段合法、ID 唯一且带 builtin- 前缀(面板据此打角标)。
func Test_DefaultRules_valid(t *testing.T) {
	known := map[rules.Type]bool{
		rules.TypeHostBlock: true, rules.TypeHostAllow: true, rules.TypeHeader: true,
		rules.TypeBodyReplace: true, rules.TypeMapLocal: true, rules.TypeSSLBypass: true,
		rules.TypeAccessControl: true, rules.TypeGateway: true, rules.TypeMirrorRemote: true,
		rules.TypeSecondaryProxy: true, rules.TypeReverseProxy: true, rules.TypeHighlight: true,
	}
	seen := map[string]bool{}
	for _, r := range rules.DefaultRules() {
		if !known[r.Type] {
			t.Errorf("%s: 未知类型 %q", r.ID, r.Type)
		}
		if seen[r.ID] {
			t.Errorf("ID 重复: %s", r.ID)
		}
		seen[r.ID] = true
		if !rules.IsBuiltinID(r.ID) {
			t.Errorf("%s: 内置规则 ID 必须带 builtin- 前缀", r.ID)
		}
		if r.Match.Path != "" {
			if _, err := regexp.Compile(r.Match.Path); err != nil {
				t.Errorf("%s: 路径正则编译失败: %v", r.ID, err)
			}
		}
		if r.BodyReplace != nil && r.BodyReplace.From == "" {
			t.Errorf("%s: body_replace 的 from 为空(恒不命中)", r.ID)
		}
		if r.Header != nil {
			switch r.Header.Phase {
			case "request", "response":
			default:
				t.Errorf("%s: header 阶段非法 %q", r.ID, r.Header.Phase)
			}
		}
		if r.Highlight != nil && r.Highlight.Color == "" {
			t.Errorf("%s: highlight 颜色为空", r.ID)
		}
	}
	if len(seen) < 8 {
		t.Errorf("内置规则只有 %d 条, 预期 >= 8", len(seen))
	}
}

// 全新数据目录: rules.json 缺失 -> 启动即写入内置规则。
func Test_NewChain_seedsDefaultsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := c.List()
	if len(list) < 8 {
		t.Fatalf("种子后只有 %d 条规则", len(list))
	}
	if _, err := os.Stat(filepath.Join(dir, "rules.json")); err != nil {
		t.Fatalf("rules.json 没落盘: %v", err)
	}
	// 再建一次 Chain(模拟重启): 不应重复种子
	c2, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.List()) != len(list) {
		t.Fatalf("重启后规则数变了: %d -> %d", len(list), len(c2.List()))
	}
}

// 用户把规则删空后: rules.json 存在且为空 -> 不许被开机复活(那是用户的意志)。
func Test_NewChain_doesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), []byte(`{"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.List()) != 0 {
		t.Fatalf("空 rules.json 被种子污染了: %d 条", len(c.List()))
	}
}

// [compat-400] 内置规则不得默认启用会改写所有请求头的全局规则。
// 引擎已在 response 阶段透明解码→替换→重编码, 不需要强制 identity;
// 去缓存/强制 identity 会让部分站点(mail.163.com)直接回 400。
func Test_DefaultRules_noGlobalRequestRewrite(t *testing.T) {
	for _, r := range rules.DefaultRules() {
		if r.Type != rules.TypeHeader || r.Header == nil || r.Header.Phase != "request" {
			continue
		}
		if r.Enabled && len(r.Match.Host) == 0 && len(r.Match.Path) == 0 && len(r.Match.Methods) == 0 {
			t.Errorf("内置规则 %q 默认启用了全局请求头改写(会导致部分站点 400)", r.ID)
		}
	}
}
