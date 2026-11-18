package proxyterm

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EnvFor returns HTTP(S)_PROXY and related vars for proxyAddr (host:port or URL).
func EnvFor(proxyAddr string, base []string) []string {
	if proxyAddr == "" {
		return append([]string(nil), base...)
	}
	url := proxyAddr
	if !strings.Contains(url, "://") {
		url = "http://" + url
	}
	out := make([]string, 0, len(base)+4)
	for _, e := range base {
		up := strings.ToUpper(e)
		if strings.HasPrefix(up, "HTTP_PROXY=") ||
			strings.HasPrefix(up, "HTTPS_PROXY=") ||
			strings.HasPrefix(up, "ALL_PROXY=") ||
			strings.HasPrefix(up, "NO_PROXY=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out,
		"HTTP_PROXY="+url,
		"HTTPS_PROXY="+url,
		"ALL_PROXY="+url,
		"NO_PROXY=localhost,127.0.0.1,::1",
	)
	return out
}

// ShellCommand returns a dry-runable shell invocation with proxy env (Linux/macOS).
func ShellCommand(proxyAddr, shell string) (*exec.Cmd, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
	}
	cmd := exec.Command(shell, "-c", "echo proxying-terminal-ok && exit 0")
	cmd.Env = EnvFor(proxyAddr, os.Environ())
	return cmd, nil
}

// SystemProxyHint returns OS-specific notes (no mutation in CI).
func SystemProxyHint() string {
	switch runtime.GOOS {
	case "linux":
		return "export HTTP_PROXY/HTTPS_PROXY or use gsettings for GNOME; proxying does not mutate system settings by default"
	case "darwin":
		return "networksetup -setwebproxy / -setsecurewebproxy; proxying does not mutate by default"
	case "windows":
		return "netsh winhttp set proxy; proxying does not mutate by default"
	default:
		return fmt.Sprintf("set HTTP_PROXY manually on %s", runtime.GOOS)
	}
}
