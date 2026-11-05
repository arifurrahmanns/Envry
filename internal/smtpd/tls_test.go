package smtpd

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"strings"
	"testing"

	"github.com/arifurrahmanns/envry/internal/store"
	"github.com/arifurrahmanns/envry/internal/tlsutil"
)

// startTLS serves one server on a plain (STARTTLS) and an implicit-TLS port.
func startTLS(t *testing.T) (st *store.Store, plain, implicit string, client *tls.Config) {
	t.Helper()
	cert, err := tlsutil.Load(tlsutil.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(context.Background(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	srv := New(st, Options{TLSConfig: cert.Config})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", cert.Config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() { done <- srv.Serve(ln) }()
	go func() { done <- srv.Serve(tlsLn) }()
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		for range 2 {
			if err := <-done; err != nil {
				t.Errorf("Serve: %v", err)
			}
		}
	})
	return st, ln.Addr().String(), tlsLn.Addr().String(), &tls.Config{RootCAs: cert.CertPool(), ServerName: "localhost"}
}

func sendOver(t *testing.T, c *smtp.Client, subject string) {
	t.Helper()
	if err := c.Mail("app@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rcpt("dev@test.local"); err != nil {
		t.Fatal(err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("Subject: " + subject + "\r\n\r\nencrypted\r\n"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Quit(); err != nil {
		t.Fatal(err)
	}
}

func TestStartTLS(t *testing.T) {
	st, plain, _, clientTLS := startTLS(t)
	c, err := smtp.Dial(plain)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		t.Fatal("STARTTLS not advertised")
	}
	if err := c.StartTLS(clientTLS); err != nil {
		t.Fatalf("STARTTLS: %v", err)
	}
	if state, ok := c.TLSConnectionState(); !ok || state.Version < tls.VersionTLS12 {
		t.Fatalf("not encrypted: %+v", state)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		t.Error("STARTTLS still advertised on an encrypted connection")
	}
	if err := c.Auth(smtp.PlainAuth("", "dev@test.local", "pw", "127.0.0.1")); err != nil {
		t.Fatalf("AUTH over TLS: %v", err)
	}
	sendOver(t, c, "Over STARTTLS")
	assertSubject(t, st, "Over STARTTLS")
}

func TestImplicitTLS(t *testing.T) {
	st, _, implicit, clientTLS := startTLS(t)
	conn, err := tls.Dial("tcp", implicit, clientTLS)
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	c, err := smtp.NewClient(conn, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		t.Error("STARTTLS advertised on the implicit-TLS port")
	}
	sendOver(t, c, "Over SMTPS")
	assertSubject(t, st, "Over SMTPS")
}

func assertSubject(t *testing.T, st *store.Store, subject string) {
	t.Helper()
	page, err := st.ListMessages(context.Background(), store.ListOptions{})
	if err != nil || page.Total != 1 || !strings.EqualFold(page.Items[0].Subject, subject) {
		t.Errorf("stored = %+v, %v", page.Items, err)
	}
}
