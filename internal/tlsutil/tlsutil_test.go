package tlsutil

import (
	"crypto/tls"
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
