package ca

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func Test_Load_generates_root_and_writes_key_0600(t *testing.T) {
	// Given
	dir := t.TempDir()

	// When
	a, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Then
	keyPath := filepath.Join(dir, caKeyFile)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat ca.key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("ca.key mode = %04o, want 0600", mode)
	}
	if len(a.RootPEM()) == 0 {
		t.Fatal("RootPEM empty")
	}
	if _, err := os.Stat(filepath.Join(dir, caCertFile)); err != nil {
		t.Fatalf("ca.pem missing: %v", err)
	}
}

func Test_Load_reload_same_subject(t *testing.T) {
	// Given
	dir := t.TempDir()
	first, err := Load(dir)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	sub1 := rootSubject(t, first.RootPEM())

	// When
	second, err := Load(dir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	sub2 := rootSubject(t, second.RootPEM())

	// Then
	if sub1 != sub2 {
		t.Errorf("subject mismatch after reload:\n  first=%q\n second=%q", sub1, sub2)
	}
	if string(first.RootPEM()) != string(second.RootPEM()) {
		t.Error("RootPEM bytes differ after reload")
	}
}

func Test_Certificate_DNSNames_contain_host(t *testing.T) {
	// Given
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const host = "example.com"

	// When
	cert, err := a.Certificate(host)
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}

	// Then
	if cert.Leaf == nil {
		t.Fatal("Leaf is nil")
	}
	found := false
	for _, n := range cert.Leaf.DNSNames {
		if n == host {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DNSNames = %v, want contain %q", cert.Leaf.DNSNames, host)
	}
}

func Test_Certificate_caches_same_pointer(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c1, err := a.Certificate("cache.test")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	c2, err := a.Certificate("cache.test")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if c1 != c2 {
		t.Error("expected same cached *tls.Certificate")
	}
}

func Test_Certificate_IP_host(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cert, err := a.Certificate("127.0.0.1")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	if cert.Leaf == nil || len(cert.Leaf.IPAddresses) == 0 {
		t.Fatalf("want IP SANs, leaf=%v", cert.Leaf)
	}
	if cert.Leaf.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("IP = %v", cert.Leaf.IPAddresses)
	}
}

func Test_Load_empty_dataDir_errors(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for empty data-dir")
	}
}

func Test_Load_enforces_key_mode_on_existing(t *testing.T) {
	// Given — generate then loosen perms
	dir := t.TempDir()
	if _, err := Load(dir); err != nil {
		t.Fatalf("generate: %v", err)
	}
	keyPath := filepath.Join(dir, caKeyFile)
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	// When
	if _, err := Load(dir); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Then
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("after reload ca.key mode = %04o, want 0600", mode)
	}
}

func rootSubject(t *testing.T, pemBytes []byte) string {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("pem decode failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cert.Subject.String()
}
