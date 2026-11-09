// Package tlsutil provides the TLS configuration shared by the SMTP, POP3
// and IMAP servers: a user-supplied certificate, or a self-signed one that
// Envry generates and keeps so mail clients only have to trust it once.
package tlsutil

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Certificates are valid for a year (within the 398-day limit some
	// clients enforce even for trusted exceptions) and replaced when fewer
	// than 30 days remain.
	validity    = 365 * 24 * time.Hour
	renewBefore = 30 * 24 * time.Hour

	CertFile = "cert.pem"
	KeyFile  = "key.pem"
)

// Options selects where the certificate comes from.
type Options struct {
	// CertFile and KeyFile, when both set, are loaded as is.
	CertFile string
	KeyFile  string
	// Dir stores the generated self-signed certificate. Empty keeps it in
	// memory only (a new certificate on every start).
	Dir string
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Result is a ready TLS configuration plus details worth logging.
type Result struct {
	Config      *tls.Config
	Fingerprint string // SHA-256 of the certificate, colon-separated hex
	SelfSigned  bool
	Generated   bool   // a new self-signed certificate was created
	Path        string // where the certificate was read from or saved to
	NotAfter    time.Time
	DNSNames    []string
}

// Load returns the TLS configuration for the given options.
func Load(opts Options) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if (opts.CertFile == "") != (opts.KeyFile == "") {
		return nil, errors.New("tls: certificate and key files must be given together")
	}
	if opts.CertFile != "" {
		pair, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls: load %s: %w", opts.CertFile, err)
		}
		return result(pair, false, false, opts.CertFile)
	}

	if opts.Dir != "" {
		certPath, keyPath := filepath.Join(opts.Dir, CertFile), filepath.Join(opts.Dir, KeyFile)
		if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
			// Older versions generated a certificate authority; it is replaced
			// by a server-only certificate before anyone is asked to trust it.
			if leaf, err := x509.ParseCertificate(pair.Certificate[0]); err == nil && !leaf.IsCA &&
				leaf.NotAfter.Sub(opts.Now()) > renewBefore {
				return result(pair, true, false, certPath)
			}
		}
	}

	certPEM, keyPEM, err := generate(opts.Now())
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("tls: generated certificate: %w", err)
	}
	path := ""
	if opts.Dir != "" {
		path = filepath.Join(opts.Dir, CertFile)
		if err := save(opts.Dir, certPEM, keyPEM); err != nil {
			return nil, err
		}
	}
	return result(pair, true, true, path)
}

func result(pair tls.Certificate, selfSigned, generated bool, path string) (*Result, error) {
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("tls: parse certificate: %w", err)
	}
	pair.Leaf = leaf
	return &Result{
		Config: &tls.Config{
			Certificates: []tls.Certificate{pair},
			MinVersion:   tls.VersionTLS12,
		},
		Fingerprint: Fingerprint(leaf),
		SelfSigned:  selfSigned,
		Generated:   generated,
		Path:        path,
		NotAfter:    leaf.NotAfter,
		DNSNames:    leaf.DNSNames,
	}, nil
}

// Fingerprint formats a certificate's SHA-256 digest as AA:BB:….
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	var b strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(h[i : i+2])
	}
	return b.String()
}

// generate creates a self-signed ECDSA P-256 certificate for the names a
// local mail client is likely to connect with. It is a server certificate,
// not a certificate authority: once trusted on a computer (so apps that
// check certificates accept Envry) it vouches for nothing but Envry itself.
func generate(now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("tls: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("tls: serial number: %w", err)
	}

	dns := []string{"localhost", "envry"}
	if host, err := os.Hostname(); err == nil && host != "" && !strings.EqualFold(host, "localhost") {
		dns = append(dns, strings.ToLower(host))
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Envry local mail server", Organization: []string{"Envry"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              dns,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("tls: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("tls: encode key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func save(dir string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tls: create %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFile), keyPEM, 0o600); err != nil {
		return fmt.Errorf("tls: save key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, CertFile), certPEM, 0o644); err != nil {
		return fmt.Errorf("tls: save certificate: %w", err)
	}
	return nil
}

// CertPool returns a pool trusting only the result's certificate, for
// clients (and tests) that connect to this server.
func (r *Result) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(r.Config.Certificates[0].Leaf)
	return pool
}

// PEM returns the certificate in PEM form.
func (r *Result) PEM() []byte {
	var b bytes.Buffer
	_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: r.Config.Certificates[0].Certificate[0]})
	return b.Bytes()
}
