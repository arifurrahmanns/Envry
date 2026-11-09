package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestGeneratedCertificateIsKeptAndRenewed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	first, err := Load(Options{Dir: dir, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if !first.SelfSigned || !first.Generated || first.Path != filepath.Join(dir, CertFile) {
		t.Errorf("first = %+v", first)
	}
	if !slices.Contains(first.DNSNames, "localhost") {
		t.Errorf("DNS names = %v", first.DNSNames)
	}
	leaf := first.Config.Certificates[0].Leaf
	if !slices.ContainsFunc(leaf.IPAddresses, func(ip net.IP) bool { return ip.Equal(net.IPv4(127, 0, 0, 1)) }) {
		t.Errorf("IPs = %v", leaf.IPAddresses)
	}
	if info, err := os.Stat(filepath.Join(dir, KeyFile)); err != nil || info.Size() == 0 {
		t.Fatalf("key not saved: %v", err)
	}

	// Restarting reuses the saved certificate, so clients keep trusting it.
	again, err := Load(Options{Dir: dir, Now: func() time.Time { return now.Add(24 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if again.Generated || again.Fingerprint != first.Fingerprint {
		t.Errorf("certificate was not reused: generated=%v", again.Generated)
	}

	// Close to expiry it is replaced.
	later, err := Load(Options{Dir: dir, Now: func() time.Time { return now.Add(validity - 10*24*time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if !later.Generated || later.Fingerprint == first.Fingerprint {
		t.Error("expiring certificate was not renewed")
	}
}

// A generated certificate can be trusted on a computer without letting it
// vouch for other sites, so it must not be a certificate authority. Older
// versions saved one that was; it is replaced on the next start.
func TestGeneratedCertificateIsNotACertificateAuthority(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	old := saveCACertificate(t, dir, now)
	r, err := Load(Options{Dir: dir, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	leaf := r.Config.Certificates[0].Leaf
	if !r.Generated || r.Fingerprint == old {
		t.Error("the old certificate authority was kept")
	}
	if leaf.IsCA || leaf.KeyUsage&x509.KeyUsageCertSign != 0 {
		t.Errorf("IsCA = %v, key usage = %v", leaf.IsCA, leaf.KeyUsage)
	}
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Errorf("extended key usage = %v", leaf.ExtKeyUsage)
	}
}

// saveCACertificate stores a self-signed certificate authority the way
// older versions generated it and returns its fingerprint.
func saveCACertificate(t *testing.T, dir string, now time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Envry local mail server"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := save(dir, certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return Fingerprint(cert)
}

func TestInMemoryCertificate(t *testing.T) {
	r, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "" || !r.Generated {
		t.Errorf("in-memory result = %+v", r)
	}
}

func TestUserCertificate(t *testing.T) {
	dir := t.TempDir()
	gen, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Load(Options{CertFile: filepath.Join(dir, CertFile), KeyFile: filepath.Join(dir, KeyFile)})
	if err != nil {
		t.Fatal(err)
	}
	if r.SelfSigned || r.Generated || r.Fingerprint != gen.Fingerprint {
		t.Errorf("user certificate = %+v", r)
	}
	if _, err := Load(Options{CertFile: "missing.pem", KeyFile: "missing.key"}); err == nil {
		t.Error("missing files accepted")
	}
	if _, err := Load(Options{CertFile: "only-cert.pem"}); err == nil {
		t.Error("certificate without key accepted")
	}
}

func TestHandshakeVerifiesAgainstPool(t *testing.T) {
	r, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", r.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.(*tls.Conn).Handshake()
			c.Close()
		}
	}()
	for _, name := range []string{"localhost", "127.0.0.1"} {
		conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: r.CertPool(), ServerName: name})
		if err != nil {
			t.Fatalf("handshake as %s: %v", name, err)
		}
		conn.Close()
	}
}
