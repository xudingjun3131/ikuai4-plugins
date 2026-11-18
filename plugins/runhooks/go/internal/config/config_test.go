package config

import (
	"flag"
	"net"
	"testing"
	"time"
)

func Test_Defaults_bindHosts_are_loopback(t *testing.T) {
	// Given
	cfg := Defaults()

	// When
	proxyHost, err := HostOf(cfg.ProxyAddr)
	if err != nil {
		t.Fatalf("proxy-addr: %v", err)
	}
	apiHost, err := HostOf(cfg.APIAddr)
	if err != nil {
		t.Fatalf("api-addr: %v", err)
	}

	// Then — security invariant: never open-bind by default
	if proxyHost != "127.0.0.1" {
		t.Errorf("proxy default host = %q, want 127.0.0.1 (not 0.0.0.0)", proxyHost)
	}
	if apiHost != "127.0.0.1" {
		t.Errorf("api default host = %q, want 127.0.0.1 (not 0.0.0.0)", apiHost)
	}
	if proxyHost == "0.0.0.0" || apiHost == "0.0.0.0" {
		t.Fatal("default bind must not be 0.0.0.0")
	}
}

func Test_Defaults_ports_and_limits(t *testing.T) {
	cfg := Defaults()
	if cfg.ProxyAddr != DefaultProxyAddr {
		t.Errorf("ProxyAddr = %q, want %q", cfg.ProxyAddr, DefaultProxyAddr)
	}
	if cfg.APIAddr != DefaultAPIAddr {
		t.Errorf("APIAddr = %q, want %q", cfg.APIAddr, DefaultAPIAddr)
	}
	if cfg.MaxFlows != DefaultMaxFlows {
		t.Errorf("MaxFlows = %d, want %d", cfg.MaxFlows, DefaultMaxFlows)
	}
	if cfg.MaxBody != DefaultMaxBody {
		t.Errorf("MaxBody = %d, want %d", cfg.MaxBody, DefaultMaxBody)
	}
	if cfg.BPMax != DefaultBPMax {
		t.Errorf("BPMax = %d, want %d", cfg.BPMax, DefaultBPMax)
	}
	if cfg.BPTimeout != 60*time.Second {
		t.Errorf("BPTimeout = %v, want 60s", cfg.BPTimeout)
	}
	if cfg.ScriptTimeout != 100*time.Millisecond {
		t.Errorf("ScriptTimeout = %v, want 100ms", cfg.ScriptTimeout)
	}
}

func Test_Parse_emptyArgs_keeps_loopback_defaults(t *testing.T) {
	// Given
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	// When
	cfg, err := Parse(fs, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Then
	for name, addr := range map[string]string{
		"proxy-addr": cfg.ProxyAddr,
		"api-addr":   cfg.APIAddr,
	} {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if host != "127.0.0.1" {
			t.Errorf("%s host = %q, want 127.0.0.1", name, host)
		}
	}
}

func Test_Parse_overrides_addrs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := Parse(fs, []string{
		"-proxy-addr", "127.0.0.1:18080",
		"-api-addr", "127.0.0.1:19090",
		"-max-flows", "100",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ProxyAddr != "127.0.0.1:18080" {
		t.Errorf("ProxyAddr = %q", cfg.ProxyAddr)
	}
	if cfg.APIAddr != "127.0.0.1:19090" {
		t.Errorf("APIAddr = %q", cfg.APIAddr)
	}
	if cfg.MaxFlows != 100 {
		t.Errorf("MaxFlows = %d", cfg.MaxFlows)
	}
}

func Test_Validate_rejects_bad_addr(t *testing.T) {
	cfg := Defaults()
	cfg.ProxyAddr = "not-a-host-port"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid proxy-addr")
	}
}
