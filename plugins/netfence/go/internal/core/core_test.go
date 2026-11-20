package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAt() time.Time { return time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC) } // Friday 10:30

// ---- FilterList: the three real-world formats + exceptions + subdomains ----

func Test_FilterList_Formats(t *testing.T) {
	f := NewFilterList()
	body := strings.Join([]string{
		"! Title: test list",
		"# comment",
		"||ads.example.com^",         // AdGuard
		"||tracker.net^$third-party", // AdGuard with options
		"0.0.0.0 malware.com",        // hosts
		"127.0.0.1 phishing.org",     // hosts
		"plain-block.net",            // plain domain
		"@@||ads.example.com^",       // exception wins over block
		"||*.wildcard.io^",           // wildcard
	}, "\n")
	n := f.ParseList("test.txt", body, CatAds)
	if n < 5 {
		t.Fatalf("parsed too few entries: %d", n)
	}
	cases := []struct {
		domain string
		want   bool
	}{
		{"ads.example.com", false},   // exception
		{"x.ads.example.com", false}, // exception on parent
		{"tracker.net", true},
		{"sub.tracker.net", true}, // suffix match
		{"malware.com", true},
		{"phishing.org", true},
		{"plain-block.net", true},
		{"a.wildcard.io", true},
		{"safe.example.org", false},
	}
	for _, c := range cases {
		got, _, _ := f.Lookup(c.domain)
		if got != c.want {
			t.Errorf("Lookup(%q)=%v want %v", c.domain, got, c.want)
		}
	}
	if blocked, src, _ := f.Lookup("tracker.net"); !blocked || src != "test.txt" {
		t.Errorf("provenance lost: blocked=%v src=%q", blocked, src)
	}
}

func Test_FilterList_LoadDirAndCategory(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "lists"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "lists", "ads.txt"), []byte("||ads.foo^\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "lists", "malware-feed.txt"), []byte("||bad.foo^\n"), 0o600)
	f := NewFilterList()
	n, err := f.LoadDir(filepath.Join(dir, "lists"))
	if err != nil || n != 2 {
		t.Fatalf("LoadDir n=%d err=%v", n, err)
	}
	if _, _, cat := f.Lookup("bad.foo"); cat != CatMalware {
		t.Errorf("category=%q want malware", cat)
	}
	if got := f.SortedDomains(); len(got) != 2 || got[0] != "ads.foo" {
		t.Errorf("SortedDomains=%v", got)
	}
}

// ---- AppID ----

func Test_AppID_Classify(t *testing.T) {
	a := NewAppID()
	cases := map[string]string{
		"douyin.com": "抖音", "v11.douyin.com": "抖音",
		"weixin.qq.com": "微信", "doc.feishu.cn": "企业办公",
	}
	for d, want := range cases {
		got, _ := a.Classify(d)
		if got != want {
			t.Errorf("Classify(%q)=%q want %q", d, got, want)
		}
	}
	if app, cat := a.Classify("unknown.invalid"); app != "" || cat != CatUnknown {
		t.Errorf("unknown classified as %q/%q", app, cat)
	}
	// custom signature extension (OAF "custom signatures" strength)
	a.Add(AppSignature{App: "内部系统", Category: CatWork, Domains: []string{"corp.internal"}})
	if app, _ := a.Classify("x.corp.internal"); app != "内部系统" {
		t.Errorf("custom signature not applied: %q", app)
	}
}

// ---- Policy: subject / schedule / priority / first-match ----

func Test_Policy_OrderAndSchedule(t *testing.T) {
	p := NewPolicy(
		Rule{ID: "allow-work", Enabled: true, Priority: 10,
			Match: Match{Apps: []string{"企业办公"}}, Action: ActionAllow},
		Rule{ID: "block-video-workhours", Enabled: true, Priority: 20,
			Subject:  Subject{IPs: []string{"192.168.1.0/24"}},
			Match:    Match{Categories: []Category{CatVideo}},
			Schedule: Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
			Action:   ActionBlock},
		Rule{ID: "default", Enabled: true, Priority: 100, Action: ActionAudit},
	)
	req := Request{ClientIP: "192.168.1.23", Domain: "v.douyin.com", At: testAt()}
	// app/category supplied by appid layer
	r, ok := p.Evaluate(req, "抖音", CatVideo)
	if !ok || r.ID != "block-video-workhours" {
		t.Fatalf("workhours block not hit: rule=%+v ok=%v", r, ok)
	}
	// outside workhours -> falls through to default
	req.At = time.Date(2026, 11, 20, 20, 0, 0, 0, time.UTC)
	if r, _ := p.Evaluate(req, "抖音", CatVideo); r.ID != "default" {
		t.Errorf("schedule ignored, got %q", r.ID)
	}
	// different subnet -> not subject
	req.At, req.ClientIP = testAt(), "192.168.9.9"
	if r, _ := p.Evaluate(req, "抖音", CatVideo); r.ID != "default" {
		t.Errorf("CIDR subject ignored, got %q", r.ID)
	}
	// work app takes precedence (priority 10)
	req.ClientIP = "192.168.1.23"
	if r, _ := p.Evaluate(req, "企业办公", CatWork); r.ID != "allow-work" {
		t.Errorf("priority ignored, got %q", r.ID)
	}
}

func Test_Policy_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := NewPolicy(Rule{ID: "r1", Enabled: true, Priority: 5,
		Match: Match{Domains: []string{"example.com"}}, Action: ActionBlock})
	if err := p.SaveFile(filepath.Join(dir, "policy.json")); err != nil {
		t.Fatal(err)
	}
	p2 := NewPolicy()
	if err := p2.LoadFile(filepath.Join(dir, "policy.json")); err != nil {
		t.Fatal(err)
	}
	if r, ok := p2.Evaluate(Request{Domain: "a.example.com", At: testAt()}, "", CatUnknown); !ok || r.ID != "r1" {
		t.Errorf("roundtrip failed: %+v %v", r, ok)
	}
}

// ---- Audit ----

func Test_Audit_RingAndQuery(t *testing.T) {
	a := NewAudit(3)
	for i := 0; i < 5; i++ {
		a.Record(Decision{Action: ActionAllow, ClientIP: "10.0.0.1", App: "抖音",
			Category: CatVideo, Domain: "douyin.com", At: testAt()})
	}
	if got := a.Query(AuditQuery{}); len(got) != 3 {
		t.Fatalf("ring cap=3 but got %d", len(got))
	}
	a.Record(Decision{Action: ActionBlock, ClientIP: "10.0.0.2", App: "微信", Category: CatSocial, At: testAt()})
	q := a.Query(AuditQuery{Action: ActionBlock})
	if len(q) != 1 || q[0].ClientIP != "10.0.0.2" {
		t.Errorf("filter by action failed: %+v", q)
	}
	s := a.Summary(time.Time{})
	if s.Total != 3 || s.Blocked != 1 || s.Clients != 2 {
		t.Errorf("summary=%+v", s)
	}
}

// ---- ACL compiler ----

func Test_RenderACL_DeterministicAndValid(t *testing.T) {
	c := ACLConfig{Table: "netfence", BlockIPs: []string{"1.2.3.4", "10.0.0.0/8"},
		AllowIPs: []string{"192.168.1.1"}, DNSPort: 18110, RedirectDNS: true,
		LANIfaces: []string{"lan1"}}
	s1, err := RenderACL(c)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := RenderACL(c)
	if s1 != s2 {
		t.Error("ACL rendering is not deterministic")
	}
	for _, want := range []string{"table inet netfence", "set block_v4", "1.2.3.4", "10.0.0.0/8",
		"ip daddr @block_v4 drop", "udp dport 53 redirect to :18110", `iifname { "lan1" }`} {
		if !strings.Contains(s1, want) {
			t.Errorf("ACL missing %q", want)
		}
	}
}

func Test_RenderACL_RejectsInjection(t *testing.T) {
	for _, bad := range []string{"1.2.3.4; nft flush ruleset", "$(reboot)", "1.2.3.4\nadd table x"} {
		if _, err := RenderACL(ACLConfig{BlockIPs: []string{bad}}); err == nil {
			t.Errorf("injection not rejected: %q", bad)
		}
	}
	if _, err := RenderACL(ACLConfig{Table: "evil; drop"}); err == nil {
		t.Error("bad table name not rejected")
	}
}

// ---- Threat intel ----

func Test_ThreatIntel_CIDRAndBadLines(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "threat"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "threat", "feed.txt"), []byte(strings.Join([]string{
		"# community feed", "1.2.3.4", "10.0.0.0/8 ; scanner range", "garbage-not-ip", "",
	}, "\n")), 0o600)
	ti := NewThreatIntel()
	added, skipped, err := ti.LoadDir(filepath.Join(dir, "threat"))
	if err != nil || added != 2 || skipped != 1 {
		t.Fatalf("added=%d skipped=%d err=%v", added, skipped, err)
	}
	if src, ok := ti.MatchIP("10.4.5.6"); !ok || src != "feed.txt" {
		t.Errorf("CIDR match failed: %q %v", src, ok)
	}
	if _, ok := ti.MatchIP("8.8.8.8"); ok {
		t.Error("unrelated IP matched")
	}
}

// ---- Engine: full pipeline integration ----

func Test_Engine_IntegratedDecisionPipeline(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"lists", "threat"} {
		_ = os.MkdirAll(filepath.Join(dir, sub), 0o700)
	}
	_ = os.WriteFile(filepath.Join(dir, "lists", "ads.txt"), []byte("||ad.example^\n@@||safe.org^\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "threat", "feed.txt"), []byte("203.0.113.0/24\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), []byte(`[
	  {"id":"block-game","name":"工作时间禁游戏","enabled":true,"priority":10,
	   "subject":{"ips":["192.168.1.0/24"]},
	   "match":{"categories":["game"]},
	   "schedule":{"days":[1,2,3,4,5],"start":"09:00","end":"18:00"},
	   "action":"block"}
	]`), 0o600)

	e, err := NewEngine(Config{Dir: dir, AuditCap: 100})
	if err != nil {
		t.Fatal(err)
	}

	check := func(name string, req Request, wantAction Action, wantLayer string) {
		t.Helper()
		req.At = testAt()
		d := e.Decide(req)
		if d.Action != wantAction || d.Layer != wantLayer {
			t.Errorf("%s: got action=%q layer=%q reason=%q; want %q/%q",
				name, d.Action, d.Layer, d.Reason, wantAction, wantLayer)
		}
	}

	check("ads blocklist", Request{ClientIP: "192.168.1.9", Domain: "x.ad.example"}, ActionBlock, "filterlist")
	check("threat IP", Request{ClientIP: "192.168.1.9", Domain: "whatever.net", DstIP: "203.0.113.7"}, ActionBlock, "threat")
	check("game policy", Request{ClientIP: "192.168.1.9", Domain: "pvp.qq.com"}, ActionBlock, "policy")
	check("video default-allow", Request{ClientIP: "192.168.1.9", Domain: "v.douyin.com"}, ActionAllow, "default")
	check("unknown default-allow", Request{ClientIP: "192.168.9.9", Domain: "unknown.invalid"}, ActionAllow, "default")
	check("exception wins", Request{ClientIP: "192.168.1.9", Domain: "safe.org"}, ActionAllow, "exception")

	// audit recorded every decision
	if got := e.Audit.Query(AuditQuery{}); len(got) != 6 {
		t.Errorf("audit records=%d want 6", len(got))
	}
	// ACL includes threat CIDR
	acl, err := e.RenderACL()
	if err != nil || !strings.Contains(acl, "203.0.113.0/24") {
		t.Errorf("ACL missing threat CIDR: err=%v", err)
	}
}

func Test_Engine_ReloadIsHot(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "lists"), 0o700)
	e, err := NewEngine(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if d := e.Decide(Request{Domain: "new.block", At: testAt()}); d.Blocked() {
		t.Fatal("unexpected pre-reload block")
	}
	_ = os.WriteFile(filepath.Join(dir, "lists", "new.txt"), []byte("||new.block^\n"), 0o600)
	if err := e.Reload(dir); err != nil {
		t.Fatal(err)
	}
	if d := e.Decide(Request{Domain: "new.block", At: testAt()}); !d.Blocked() || d.Layer != "filterlist" {
		t.Errorf("hot reload did not take effect: %+v", d)
	}
}
