// Package config parses CLI flags and holds runtime defaults for proxying.
package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Default bind addresses (loopback only — security invariant for todo 3).
const (
	DefaultProxyAddr       = "127.0.0.1:18070"
	DefaultAPIAddr         = "127.0.0.1:18071"
	DefaultTransparentAddr = "127.0.0.1:18072"
	DefaultMaxFlows        = 5000
	DefaultMaxBody         = 1 << 20 // 1048576
	DefaultBPMax           = 32
)

// Config is the process runtime configuration (CLI boundary).
type Config struct {
	ProxyAddr       string
	APIAddr         string
	DataDir         string
	MaxFlows        int
	MaxBody         int64
	BPMax           int
	BPTimeout       time.Duration
	ScriptTimeout   time.Duration
	ShowVersion     bool
	Turbo           bool
	ReportToken     string
	MCPStdio        bool
	Transparent     bool   // iKuai 插件版: 透明模式(配合 iptables REDIRECT, 无需客户端配代理)
	TransparentAddr string // iKuai 插件版: 透明监听地址(默认同 ProxyAddr; 双监听时必须分开)
	ScopeFile       string // iKuai 插件版: 域名拦截范围配置(domain_mode/domains), SIGHUP 热重载
}

// Defaults returns loopback-safe defaults (data dir under $HOME/.proxying).
func Defaults() Config {
	return Config{
		ProxyAddr:     DefaultProxyAddr,
		APIAddr:       DefaultAPIAddr,
		DataDir:       defaultDataDir(),
		MaxFlows:      DefaultMaxFlows,
		MaxBody:       DefaultMaxBody,
		BPMax:         DefaultBPMax,
		BPTimeout:     60 * time.Second,
		ScriptTimeout: 100 * time.Millisecond,
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".proxying"
	}
	return filepath.Join(home, ".proxying")
}

// Parse registers flags on fs, parses args, and returns a validated Config.
// args should be os.Args[1:] (or a test slice). Empty args yield Defaults.
func Parse(fs *flag.FlagSet, args []string) (Config, error) {
	if fs == nil {
		fs = flag.NewFlagSet("proxying", flag.ContinueOnError)
	}
	cfg := Defaults()
	fs.StringVar(&cfg.ProxyAddr, "proxy-addr", cfg.ProxyAddr, "MITM proxy listen address")
	fs.StringVar(&cfg.APIAddr, "api-addr", cfg.APIAddr, "control API listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "data directory (CA, store)")
	fs.IntVar(&cfg.MaxFlows, "max-flows", cfg.MaxFlows, "max in-memory capture flows")
	fs.Int64Var(&cfg.MaxBody, "max-body", cfg.MaxBody, "max captured body bytes per message")
	fs.IntVar(&cfg.BPMax, "bp-max", cfg.BPMax, "max concurrent breakpoints")
	fs.DurationVar(&cfg.BPTimeout, "bp-timeout", cfg.BPTimeout, "breakpoint wait timeout")
	fs.DurationVar(&cfg.ScriptTimeout, "script-timeout", cfg.ScriptTimeout, "script execution timeout")
	fs.BoolVar(&cfg.ShowVersion, "version", false, "print version and exit")
	fs.BoolVar(&cfg.Turbo, "turbo", false, "low-overhead capture (smaller default body/flow caps if still default)")
	fs.StringVar(&cfg.ReportToken, "report-token", "", "optional shared secret for POST /api/report")
	fs.BoolVar(&cfg.MCPStdio, "mcp-stdio", false, "run as MCP server over stdin/stdout ( forego HTTP UI )")
	fs.BoolVar(&cfg.Transparent, "transparent", false, "transparent mode: proxy listener uses SO_ORIGINAL_DST (for iptables REDIRECT)")
	fs.StringVar(&cfg.TransparentAddr, "transparent-addr", "", "transparent listener addr (default: same as -proxy-addr; set separately to run both)")
	fs.StringVar(&cfg.ScopeFile, "scope-file", "", "domain intercept scope config (JSON: domain_mode all_except|only, domains []); SIGHUP reloads")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if cfg.Turbo {
		if cfg.MaxBody == DefaultMaxBody {
			cfg.MaxBody = 64 << 10 // 64KiB
		}
		if cfg.MaxFlows == DefaultMaxFlows {
			cfg.MaxFlows = 1000
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks listen addresses are host:port form.
func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.ProxyAddr); err != nil {
		return fmt.Errorf("proxy-addr %q: %w", c.ProxyAddr, err)
	}
	if _, _, err := net.SplitHostPort(c.APIAddr); err != nil {
		return fmt.Errorf("api-addr %q: %w", c.APIAddr, err)
	}
	if c.MaxFlows < 1 {
		return fmt.Errorf("max-flows must be >= 1")
	}
	if c.MaxBody < 0 {
		return fmt.Errorf("max-body must be >= 0")
	}
	if c.BPMax < 1 {
		return fmt.Errorf("bp-max must be >= 1")
	}
	if c.BPTimeout <= 0 {
		return fmt.Errorf("bp-timeout must be > 0")
	}
	if c.ScriptTimeout <= 0 {
		return fmt.Errorf("script-timeout must be > 0")
	}
	if c.DataDir == "" {
		return fmt.Errorf("data-dir must be non-empty")
	}
	return nil
}

// HostOf returns the host portion of a host:port address.
func HostOf(addr string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	return host, nil
}
