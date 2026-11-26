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
	"time"
)

const (
	caCertFile = "ca.pem"
	caKeyFile  = "ca.key"
	keyMode    = 0o600
	dirMode    = 0o700

	// leafRefreshSkew: 剩余有效期不足 1h 就提前重签, 避免握手瞬间刚好跨过
	// NotAfter。leaf 本身只有 24h, 引擎长期运行时必须按需刷新。
	leafRefreshSkew = time.Hour
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
//
// [leaf-expiry] leaf 证书只有 leafValidFor(24h), 但引擎常连续运行数天。旧实现缓存
// 命中即返回, 从不检查 NotAfter → 超过 24h 后所有 MITM 证书过期: 浏览器报
// ERR_CERT_DATE_INVALID, HSTS 站点(github/google 等)不允许「继续前往」直接打不开,
// 非 HSTS 站点可手动绕过故看似正常。必须按需重签。
func (a *Authority) Certificate(host string) (*tls.Certificate, error) {
	if a == nil {
		return nil, errors.New("ca: nil authority")
	}
	host = normalizeHost(host)
	if host == "" {
		return nil, errors.New("ca: empty host")
	}

	a.mu.RLock()
	if cert, ok := a.leaves[host]; ok && !leafNeedsRefresh(cert, time.Now()) {
		a.mu.RUnlock()
		return cert, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	// double-check: 可能另一个 goroutine 刚重签过。
	if cert, ok := a.leaves[host]; ok && !leafNeedsRefresh(cert, time.Now()) {
		return cert, nil
	}

	cert, err := a.mintLeaf(host)
	if err != nil {
		return nil, err
	}
	a.leaves[host] = cert
	return cert, nil
}

// leafNeedsRefresh 报告缓存证书是否需要重签(已过期 / 剩余不足刷新阈值 / 无 Leaf)。
func leafNeedsRefresh(cert *tls.Certificate, now time.Time) bool {
	if cert == nil || cert.Leaf == nil {
		return true
	}
	if now.Before(cert.Leaf.NotBefore) {
		return true
	}
	return now.Add(leafRefreshSkew).After(cert.Leaf.NotAfter)
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
