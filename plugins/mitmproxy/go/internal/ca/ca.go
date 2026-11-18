// Package ca manages the local root CA and host leaf certificates for MITM.
// Private keys are never logged; ca.key is written with mode 0600.
package ca

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	caCertFile = "ca.pem"
	caKeyFile  = "ca.key"
	keyMode    = 0o600
	dirMode    = 0o700
)

// Authority is a generate-or-load root CA with an in-memory leaf cache.
type Authority struct {
	dataDir  string
	rootCert *x509.Certificate
	rootKey  crypto.Signer
	rootPEM  []byte

	mu     sync.RWMutex
	leaves map[string]*tls.Certificate
}

// Load opens dataDir, loading ca.pem/ca.key if present or generating a new root.
// The private key file is always written (or rewritten on generate) with mode 0600.
func Load(dataDir string) (*Authority, error) {
	if dataDir == "" {
		return nil, errors.New("ca: data-dir must be non-empty")
	}
	if err := os.MkdirAll(dataDir, dirMode); err != nil {
		return nil, fmt.Errorf("ca: mkdir %s: %w", dataDir, err)
	}

	certPath := filepath.Join(dataDir, caCertFile)
	keyPath := filepath.Join(dataDir, caKeyFile)

	if fileExists(certPath) && fileExists(keyPath) {
		return loadFromDisk(dataDir, certPath, keyPath)
	}
	return generateAndPersist(dataDir, certPath, keyPath)
}

// RootPEM returns the PEM-encoded root CA certificate (public only).
func (a *Authority) RootPEM() []byte {
	if a == nil {
		return nil
	}
	out := make([]byte, len(a.rootPEM))
	copy(out, a.rootPEM)
	return out
}

// RootCertPath returns the path to ca.pem under the data directory.
func (a *Authority) RootCertPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.dataDir, caCertFile)
}

// Certificate returns a tls.Certificate for host (DNS or IP), cached in memory.
func (a *Authority) Certificate(host string) (*tls.Certificate, error) {
	if a == nil {
		return nil, errors.New("ca: nil authority")
	}
	host = normalizeHost(host)
	if host == "" {
		return nil, errors.New("ca: empty host")
	}

	a.mu.RLock()
	if cert, ok := a.leaves[host]; ok {
		a.mu.RUnlock()
		return cert, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	if cert, ok := a.leaves[host]; ok {
		return cert, nil
	}

	cert, err := a.mintLeaf(host)
	if err != nil {
		return nil, err
	}
	a.leaves[host] = cert
	return cert, nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.ToLower(host)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
