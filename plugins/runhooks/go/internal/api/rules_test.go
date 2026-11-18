package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GitterBois/runhooks/internal/rules"
)

func Test_Rules_CRUD(t *testing.T) {
	dir := t.TempDir()
	chain, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	var reloads int
	mux := NewMux(Deps{
		Version:       "t",
		Rules:         chain,
		OnRulesChange: func() { reloads++ },
	})
	body := `{"enabled":true,"type":"host_block","match":{"host":"bad.test"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/rules", bytes.NewReader([]byte(body)))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST %d %s", rr.Code, rr.Body.String())
	}
	var created rules.Rule
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("empty id")
	}
}

// PUT /api/rules = 载入内置规则集(规则面板「内置规则」按钮)。
// 必须整包替换 + 落盘 + 触发 OnRulesChange(前端据此刷新列表)。
func Test_Rules_LoadBuiltin(t *testing.T) {
	dir := t.TempDir()
	chain, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 先造一条用户规则, 验证会被整包替换掉
	if err := chain.Save([]rules.Rule{{ID: "u1", Enabled: true, Type: rules.TypeHostBlock, Match: rules.Match{Host: "x.test"}}}); err != nil {
		t.Fatal(err)
	}
	var reloads int
	mux := NewMux(Deps{Version: "t", Rules: chain, OnRulesChange: func() { reloads++ }})
	req := httptest.NewRequest(http.MethodPut, "/api/rules", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT %d %s", rr.Code, rr.Body.String())
	}
	if reloads != 1 {
		t.Fatalf("OnRulesChange 没触发(reloads=%d)", reloads)
	}
	var out struct {
		Rules  []rules.Rule `json:"rules"`
		Loaded string       `json:"loaded"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Loaded != "builtin" {
		t.Fatalf("loaded=%q", out.Loaded)
	}
	if len(out.Rules) < 8 {
		t.Fatalf("载入后只有 %d 条", len(out.Rules))
	}
	for _, r := range out.Rules {
		if r.ID == "u1" {
			t.Fatal("用户规则没被替换掉")
		}
	}
	// 落盘验证: 重启后仍在
	c2, err := rules.NewChain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.List()) != len(out.Rules) {
		t.Fatalf("落盘数量不符: %d vs %d", len(c2.List()), len(out.Rules))
	}
}
