package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// 订阅层(rules.sub.json)与用户层(rules.json)必须物理分离:
// Save() 不落订阅规则, SaveSubRules() 只写订阅层, Reload 合并(用户在前)。
func TestSubRulesSeparation(t *testing.T) {
	dir := t.TempDir()
	chain, err := NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 用户规则
	if err := chain.Save([]Rule{{ID: "u1", Enabled: true, Type: TypeHeader, Match: Match{Host: "a.com"}}}); err != nil {
		t.Fatal(err)
	}
	// 订阅规则(不带前缀也应被自动加上)
	if err := chain.SaveSubRules([]Rule{{ID: "x1", Enabled: true, Type: TypeSSLBypass, Match: Match{Host: "b.com"}}}); err != nil {
		t.Fatal(err)
	}
	list := chain.List()
	if len(list) != 2 || list[0].ID != "u1" || list[1].ID != "sub-x1" {
		t.Fatalf("合并后规则 = %+v", list)
	}
	// 用户层文件里不能有订阅规则
	b, err := os.ReadFile(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); contains(got, "sub-x1") {
		t.Errorf("rules.json 混入订阅规则: %s", got)
	}
	// 订阅层文件存在且只有订阅规则
	b2, err := os.ReadFile(filepath.Join(dir, "rules.sub.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b2); !contains(got, "sub-x1") || contains(got, `"u1"`) {
		t.Errorf("rules.sub.json 内容不对: %s", got)
	}
	// 从 WebUI 整包保存(含订阅规则混在内)也不能把订阅规则写进用户层
	if err := chain.Save([]Rule{{ID: "u2", Enabled: true, Type: TypeHeader}, {ID: "sub-x1", Enabled: true, Type: TypeSSLBypass}}); err != nil {
		t.Fatal(err)
	}
	list = chain.List()
	if len(list) != 2 || list[0].ID != "u2" || list[1].ID != "sub-x1" {
		t.Fatalf("Save 后合并规则 = %+v", list)
	}
	b3, _ := os.ReadFile(filepath.Join(dir, "rules.json"))
	if got := string(b3); contains(got, "sub-x1") {
		t.Errorf("Save 把订阅规则落进了 rules.json: %s", got)
	}
	// 清空订阅层
	if err := chain.SaveSubRules(nil); err != nil {
		t.Fatal(err)
	}
	if list := chain.List(); len(list) != 1 || list[0].ID != "u2" {
		t.Fatalf("清空订阅层后 = %+v", list)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
