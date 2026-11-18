package ca

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

const (
	rootValidFor      = 10 * 365 * 24 * time.Hour
	leafValidFor      = 24 * time.Hour
	leafNotBeforeSkew = 5 * time.Minute
)

func loadFromDisk(dataDir, certPath, keyPath string) (*Authority, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("ca: read cert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("ca: read key: %w", err)
	}
	// Enforce 0600 on existing key (e.g. restored from backup).
	if err := os.Chmod(keyPath, keyMode); err != nil {
		return nil, fmt.Errorf("ca: chmod key: %w", err)
	}

	cert, key, err := parseRoot(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &Authority{
		dataDir:  dataDir,
		rootCert: cert,
		rootKey:  key,
		rootPEM:  certPEM,
		leaves:   make(map[string]*tls.Certificate),
	}, nil
}

func generateAndPersist(dataDir, certPath, keyPath string) (*Authority, error) {
	cert, key, certPEM, keyPEM, err := mintRoot()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("ca: write cert: %w", err)
	}
	if err := writeKeyFile(keyPath, keyPEM); err != nil {
		return nil, err
	}
	return &Authority{
		dataDir:  dataDir,
		rootCert: cert,
		rootKey:  key,
		rootPEM:  certPEM,
		leaves:   make(map[string]*tls.Certificate),
	}, nil
}

func writeKeyFile(path string, keyPEM []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, keyMode)
	if err != nil {
		return fmt.Errorf("ca: create key: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(keyPEM); err != nil {
		return fmt.Errorf("ca: write key: %w", err)
	}
	if err := f.Chmod(keyMode); err != nil {
		return fmt.Errorf("ca: chmod key: %w", err)
	}
	return nil
}

func parseRoot(certPEM, keyPEM []byte) (*x509.Certificate, crypto.Signer, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, errors.New("ca: invalid ca.pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("ca: parse cert: %w", err)
	}

	kblock, _ := pem.Decode(keyPEM)
	if kblock == nil {
		return nil, nil, errors.New("ca: invalid ca.key")
	}
	var key crypto.Signer
	switch kblock.Type {
	case "EC PRIVATE KEY":
		k, err := x509.ParseECPrivateKey(kblock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("ca: parse ec key: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		pk, err := x509.ParsePKCS8PrivateKey(kblock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("ca: parse pkcs8 key: %w", err)
		}
		s, ok := pk.(crypto.Signer)
		if !ok {
			return nil, nil, errors.New("ca: key is not a signer")
		}
		key = s
	default:
		return nil, nil, fmt.Errorf("ca: unsupported key type %q", kblock.Type)
	}
	return cert, key, nil
}

func mintRoot() (*x509.Certificate, crypto.Signer, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("ca: generate root key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"proxying"},
			CommonName:   "proxying Root CA",
		},
		NotBefore:             now.Add(-leafNotBeforeSkew),
		NotAfter:              now.Add(rootValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("ca: create root cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("ca: parse root cert: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("ca: marshal root key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return cert, key, certPEM, keyPEM, nil
}

func (a *Authority) mintLeaf(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ca: generate leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"proxying"},
			CommonName:   host,
		},
		NotBefore:             now.Add(-leafNotBeforeSkew),
		NotAfter:              now.Add(leafValidFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.rootCert, &key.PublicKey, a.rootKey)
	if err != nil {
		return nil, fmt.Errorf("ca: create leaf cert: %w", err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// Chain: leaf then root (common for tls.X509KeyPair consumers).
	chainPEM := append(leafPEM, a.rootPEM...)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("ca: marshal leaf key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(chainPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("ca: leaf key pair: %w", err)
	}
	if tlsCert.Leaf == nil {
		tlsCert.Leaf, err = x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("ca: parse leaf: %w", err)
		}
	}
	return &tlsCert, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("ca: serial: %w", err)
	}
	if n.Sign() == 0 {
		n = big.NewInt(1)
	}
	return n, nil
}
