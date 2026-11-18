package proxyterm_test

import (
	"strings"
	"testing"

	"github.com/GitterBois/runhooks/internal/proxyterm"
)

func Test_EnvFor_sets_proxy(t *testing.T) {
	env := proxyterm.EnvFor("127.0.0.1:8080", []string{"FOO=1", "HTTP_PROXY=old"})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HTTP_PROXY=http://127.0.0.1:8080") {
		t.Fatalf("%s", joined)
	}
	if strings.Contains(joined, "HTTP_PROXY=old") {
		t.Fatal("old proxy not replaced")
	}
	if !strings.Contains(joined, "FOO=1") {
		t.Fatal("lost FOO")
	}
}

func Test_ShellCommand_dry_run(t *testing.T) {
	cmd, err := proxyterm.ShellCommand("127.0.0.1:8080", "/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.Contains(string(out), "proxying-terminal-ok") {
		t.Fatalf("%s", out)
	}
}

func Test_SystemProxyHint_nonempty(t *testing.T) {
	if proxyterm.SystemProxyHint() == "" {
		t.Fatal("empty hint")
	}
}
