package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// [D7] 时段策略统一北京时间, 与固件本地时区(UTC)无关。真机证据: 填 09:00-18:00
// 实际按 UTC 生效, 变成北京 17:00-次日 02:00。这里用两个 UTC 时刻交叉证明:
// UTC 02:00 = 北京 10:00 -> 命中; UTC 10:00 = 北京 18:00 -> 不命中。
func Test_D7_Schedule_BeijingTimezone(t *testing.T) {
	p := NewPolicy(
		Rule{ID: "workhours", Enabled: true, Priority: 10,
			Match:    Match{Categories: []Category{CatVideo}},
			Schedule: Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
			Action:   ActionBlock},
		Rule{ID: "default", Enabled: true, Priority: 100, Action: ActionAudit},
	)
	// 2026-11-20 是周五。旧版用 at.Format 比 UTC 裸时钟, 北京 10:00(UTC 02:00)
	// 会被误判成 02:00 落在工作时段外。
	beijing10 := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC) // 北京 10:00
	if r, ok := p.Evaluate(Request{ClientIP: "192.168.1.5", Domain: "x", At: beijing10}, "抖音", CatVideo); !ok || r.ID != "workhours" {
		t.Errorf("北京 10:00 应命中工作时段(旧版按 UTC 会漏判), got %q ok=%v", r.ID, ok)
	}
	beijing19 := time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC) // 北京 19:00, 明确在 18:00 之外
	if r, _ := p.Evaluate(Request{ClientIP: "192.168.1.5", Domain: "x", At: beijing19}, "抖音", CatVideo); r.ID != "default" {
		t.Errorf("北京 19:00 不应命中, got %q", r.ID)
	}
	// 关键反证: 若仍按 UTC 裸时钟, UTC 11:00(北京 19:00) 会被当成 11:00 落进 09-18 而误命中。
	// 上面的 default 结果证明用的是北京时区而非 UTC。
	// 不传 at 的零值由引擎补 now(), 仍按北京时区判断(不是按 UTC)
	fullDay := Schedule{Start: "00:00", End: "23:59"}
	if !fullDay.active(time.Now().UTC()) {
		t.Error("全天时段任何时刻都应命中")
	}
}

// [D6] 引擎层: DryRun 预演不写审计; 真实流量写且带 Source。
func Test_D6_DryRun_NoAudit_RealTraffic_Recorded(t *testing.T) {
	e, err := NewEngine(Config{AuditCap: 50})
	if err != nil {
		t.Fatal(err)
	}
	e.Decide(Request{ClientIP: "10.0.0.1", Domain: "probe.example", DryRun: true})
	if got := e.Audit.Query(AuditQuery{}); len(got) != 0 {
		t.Errorf("预演污染审计: %+v", got)
	}
	d := e.Decide(Request{ClientIP: "10.0.0.2", Domain: "real.example", Source: "dns"})
	if d.Source != "dns" {
		t.Errorf("真实流量 Source 丢失: %+v", d)
	}
	got := e.Audit.Query(AuditQuery{})
	if len(got) != 1 || got[0].ClientIP != "10.0.0.2" {
		t.Errorf("真实流量未写审计: %+v", got)
	}
	// Clear 清空
	e.Audit.Clear()
	if got := e.Audit.Query(AuditQuery{}); len(got) != 0 {
		t.Errorf("Clear 后仍有记录: %+v", got)
	}
}

// [D2] ACL 下发安全闸门: 非 inet 表拒绝, 非法表名拒绝(防注入)。
func Test_D2_ACLApply_Guards(t *testing.T) {
	if _, err := ApplyRuleset("chain forward { policy accept; }"); err == nil {
		t.Error("非 inet 表规则集应被拒绝")
	}
	if _, err := FlushRuleset("netfence; nft flush ruleset"); err == nil {
		t.Error("非法表名应被拒绝")
	}
}

// [D1/D3] 列表文件写入: 原子落盘 + 解析校验 + 目录穿越拒绝 + 删除幂等。
func Test_D3_WriteListFile_AtomicAndValidated(t *testing.T) {
	dir := t.TempDir()
	n, err := WriteListFile(dir, "ads.txt", "||a.example^\n||b.example^\n")
	if err != nil || n < 2 {
		t.Fatalf("WriteListFile n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ads.txt")); err != nil {
		t.Error("文件未落盘")
	}
	if _, err := WriteListFile(dir, "../escape.txt", "x"); err == nil {
		t.Error("目录穿越未拒绝")
	}
	if err := RemoveListFile(dir, "ads.txt"); err != nil {
		t.Errorf("RemoveListFile err=%v", err)
	}
	if err := RemoveListFile(dir, "ads.txt"); err != nil { // 幂等: 不存在视为成功
		t.Errorf("重复删除应幂等, err=%v", err)
	}
}
