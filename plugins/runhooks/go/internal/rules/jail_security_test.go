package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GitterBois/runhooks/internal/rules"
)

func newJail(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	jail := filepath.Join(root, "map-local")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	return jail
}

// 回归基线: 词法越界仍被拒绝(v1.1.7 既有行为不回退)。
func TestResolveMapLocal_LexicalEscapesRejected(t *testing.T) {
	jail := newJail(t)
	for _, rel := range []string{"../secret", "a/../../secret", "/etc/passwd", "..", ""} {
		if _, err := rules.ResolveMapLocal(jail, rel); err == nil {
			t.Fatalf("lexical escape %q must be rejected", rel)
		}
	}
}

func TestResolveMapLocal_NormalFileOK(t *testing.T) {
	jail := newJail(t)
	p := filepath.Join(jail, "ok.txt")
	if err := os.WriteFile(p, []byte("mapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := rules.ResolveMapLocal(jail, "ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(got)
	if err != nil || string(b) != "mapped" {
		t.Fatalf("resolved path unusable: %q err=%v", got, err)
	}
}

// v1.1.7 缺口: jail 内软链指向 jail 外, 旧实现会放行并读到越界文件。
func TestResolveMapLocal_SymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	jail := filepath.Join(root, "map-local")
	outside := filepath.Join(root, "outside.txt")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(jail, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := rules.ResolveMapLocal(jail, "escape"); err == nil {
		t.Fatal("symlink pointing outside jail must be rejected")
	}
}

// 父目录软链 + 目标尚不存在: EvalSymlinks(full) 会失败, 但父目录仍会重定向。
func TestResolveMapLocal_ParentDirSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	jail := filepath.Join(root, "map-local")
	outsideDir := filepath.Join(root, "outside-dir")
	if err := os.MkdirAll(jail, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(jail, "linkdir")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := rules.ResolveMapLocal(jail, "linkdir/not-yet-created.txt"); err == nil {
		t.Fatal("parent-dir symlink escape must be rejected even when target is missing")
	}
}

// jail 内的软链指向 jail 内另一个文件: 合法, 不应误杀。
func TestResolveMapLocal_InternalSymlinkOK(t *testing.T) {
	jail := newJail(t)
	if err := os.WriteFile(filepath.Join(jail, "real.txt"), []byte("inner"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(jail, "real.txt"), filepath.Join(jail, "alias.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, err := rules.ResolveMapLocal(jail, "alias.txt")
	if err != nil {
		t.Fatalf("internal symlink wrongly rejected: %v", err)
	}
	if !strings.HasPrefix(got, filepath.Clean(jail)+string(filepath.Separator)) {
		t.Fatalf("resolved outside jail: %q", got)
	}
}

// jail 本身是软链时, 合法文件不能被误判成越界。
func TestResolveMapLocal_JailItselfSymlink(t *testing.T) {
	root := t.TempDir()
	realJail := filepath.Join(root, "real-jail")
	linkedJail := filepath.Join(root, "map-local")
	if err := os.MkdirAll(realJail, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realJail, "ok.txt"), []byte("mapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realJail, linkedJail); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, err := rules.ResolveMapLocal(linkedJail, "ok.txt")
	if err != nil {
		t.Fatalf("jail symlink caused false positive: %v", err)
	}
	b, err := os.ReadFile(got)
	if err != nil || string(b) != "mapped" {
		t.Fatalf("resolved path unusable: %q err=%v", got, err)
	}
}
