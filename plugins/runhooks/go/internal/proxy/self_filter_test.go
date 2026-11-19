package proxy

import (
	"net"
	"testing"

	"github.com/GitterBois/runhooks/internal/store"
)

func firstLocalNonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("InterfaceAddrs unavailable: %v", err)
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		v4 := ipNet.IP.To4()
		if v4 == nil || v4.IsLoopback() {
			continue
		}
		return v4.String()
	}
	t.Skip("no non-loopback IPv4 on host")
	return ""
}

func Test_isSelfClient(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:54321", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"[::1]:1234", true},
		{"10.10.10.99:443", false}, // 局域网其它设备, 必须保留
		{"", false},
		{"not-an-ip:80", false},
	}
	for _, c := range cases {
		if got := isSelfClient(c.addr); got != c.want {
			t.Errorf("isSelfClient(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
	if self := firstLocalNonLoopbackIPv4(t); self != "" {
		if !isSelfClient(self+":18062") {
			t.Errorf("isSelfClient(own LAN IP %q) = false, want true", self)
		}
	}
}

// 真机故障复现: 路由器自己探 18062 的 error flow 不得落库; 真实设备的流必须落库。
func Test_record_skips_self_probe_but_keeps_lan_clients(t *testing.T) {
	// 包级 TestMain 为既有单测打开了逃生开关, 本用例要验的是生产默认行为,
	// 这里显式关掉。
	t.Setenv("RUNHOOKS_RECORD_SELF_TRAFFIC", "0")
	sink := store.New(10)
	e := &Engine{sink: sink}
	self := firstLocalNonLoopbackIPv4(t)

	probe := store.Flow{
		State:      store.StateError,
		ClientAddr: self + ":18062",
		Request:    store.Message{Method: "GET", URL: "https://" + self + ":18062/"},
		Error:      "tls handshake failed",
	}
	if _, ok := e.record(probe); ok {
		t.Fatal("self-probe flow was recorded")
	}
	if sink.Len() != 0 {
		t.Fatalf("sink has %d flows after self-probe, want 0", sink.Len())
	}

	lan := store.Flow{
		State:      store.StateDone,
		ClientAddr: "10.10.10.99:54321",
		Request:    store.Message{Method: "GET", URL: "http://example.com/"},
	}
	if _, ok := e.record(lan); !ok {
		t.Fatal("LAN client flow was dropped")
	}
	if sink.Len() != 1 {
		t.Fatalf("sink has %d flows after LAN flow, want 1", sink.Len())
	}
}

// 逃生开关: 本地从 127.0.0.1 造流的冒烟测试仍能记录。
func Test_record_escape_switch_allows_self_traffic(t *testing.T) {
	t.Setenv("RUNHOOKS_RECORD_SELF_TRAFFIC", "1")
	sink := store.New(10)
	e := &Engine{sink: sink}
	f := store.Flow{
		ClientAddr: "127.0.0.1:54321",
		Request:    store.Message{Method: "GET", URL: "http://example.com/"},
	}
	if _, ok := e.record(f); !ok {
		t.Fatal("escape switch should allow loopback flow")
	}
	if sink.Len() != 1 {
		t.Fatalf("sink has %d flows, want 1", sink.Len())
	}
}
