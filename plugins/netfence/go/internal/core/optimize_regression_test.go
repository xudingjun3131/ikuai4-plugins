package core

import (
	"strings"
	"sync"
	"testing"
)

// 本文件覆盖 v1.1.2 优化项: Lookup 零分配后缀扫描、ParseList 一次性持锁、
// wildcard 跨列表去重, 并保证既有语义(后缀/例外/wildcard)不变。

func TestOptimize_LookupSuffixAndWildcardUnchanged(t *testing.T) {
	f := NewFilterList()
	body := strings.Join([]string{
		"||ads.example.com^",
		"@@||ok.ads.example.com^",
		"||*.wild.io^",
	}, "\n")
	if n := f.ParseList("t.txt", body, CatAds); n != 3 {
		t.Fatalf("ParseList n=%d want 3", n)
	}
	cases := []struct {
		d    string
		want bool
	}{
		{"ads.example.com", true},
		{"x.ads.example.com", true},   // 后缀命中
		{"ok.ads.example.com", false}, // 例外优先
		{"a.wild.io", true},           // wildcard 用完整域名匹配
		{"safe.example.org", false},
	}
	for _, c := range cases {
		if got, _, _ := f.Lookup(c.d); got != c.want {
			t.Errorf("Lookup(%q)=%v want %v", c.d, got, c.want)
		}
	}
}

func TestOptimize_LookupNoAllocOnMiss(t *testing.T) {
	f := NewFilterList()
	f.ParseList("t.txt", "||blocked.example^\n", CatAds)
	allocs := testing.AllocsPerRun(1000, func() {
		f.Lookup("a.b.c.notblocked.example.org")
	})
	if allocs > 0 {
		t.Errorf("Lookup miss allocs=%v, want 0 (Split/Join 已移除)", allocs)
	}
}

func TestOptimize_WildcardDedupAcrossCalls(t *testing.T) {
	f := NewFilterList()
	f.ParseList("a.txt", "||*.wild.io^\n||*.wild.io^\n", CatAds)
	f.ParseList("b.txt", "||*.wild.io^\n", CatAds)
	if got := f.Stats()["wildcard"]; got != 1 {
		t.Errorf("wildcard=%d want 1", got)
	}
}

// go test -race 下验证 ParseList 改为整段一次持锁后仍并发安全。
func TestOptimize_ParseListConcurrentSafe(t *testing.T) {
	f := NewFilterList()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f.ParseList("x.txt", "||d"+string(rune('a'+i))+".example^\n", CatAds)
			for j := 0; j < 100; j++ {
				f.Lookup("sub.d.example")
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkLookup_Suffix(b *testing.B) {
	f := NewFilterList()
	f.ParseList("bench.txt", "||blocked.example^\n", CatAds)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f.Lookup("a.b.c.notblocked.example.org")
	}
}
