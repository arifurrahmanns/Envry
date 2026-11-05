package pop3d

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/store"
	"github.com/arifurrahmanns/envry/internal/tlsutil"
)

// startTLS serves one server on a plain (STLS) and an implicit-TLS port.
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
		st.Close()
	})
	return st, ln.Addr().String(), tlsLn.Addr().String(), &tls.Config{RootCAs: cert.CertPool(), ServerName: "localhost"}
}

func capa(t *testing.T, c *client) []string {
	t.Helper()
	status, lines := c.multi("CAPA")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("CAPA = %q", status)
	}
	return lines
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestSTLS(t *testing.T) {
	st, plain, _, clientTLS := startTLS(t)
	deliver(t, st, "dev@test.local", "Secret", "encrypted body\r\n")

	c := dial(t, plain)
	if !has(capa(t, c), "STLS") {
		t.Fatal("STLS not advertised")
	}
	if r := c.cmd("STLS"); r != "+OK Begin TLS negotiation" {
		t.Fatalf("STLS = %q", r)
	}
	tlsConn := tls.Client(c.conn, clientTLS)
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	c.conn, c.r = tlsConn, bufio.NewReader(tlsConn)

	if has(capa(t, c), "STLS") {
		t.Error("STLS advertised on an encrypted connection")
	}
	if r := c.cmd("STLS"); r != "-ERR connection is already encrypted" {
		t.Errorf("second STLS = %q", r)
	}
	c.login("dev@test.local")
	if _, lines := c.multi("RETR 1"); !has(lines, "encrypted body") {
		t.Errorf("RETR over TLS = %q", lines)
	}
}

func TestSTLSRejectedAfterLogin(t *testing.T) {
	_, plain, _, _ := startTLS(t)
	c := dial(t, plain)
	c.login("dev@test.local")
	if r := c.cmd("STLS"); r != "-ERR STLS is only valid before login" {
		t.Errorf("STLS after login = %q", r)
	}
}

func TestSTLSWithoutTLSConfigured(t *testing.T) {
	_, addr := start(t, Options{})
	c := dial(t, addr)
	if has(capa(t, c), "STLS") {
		t.Error("STLS advertised without a certificate")
	}
	if r := c.cmd("STLS"); r != "-ERR TLS is not enabled on this server" {
		t.Errorf("STLS = %q", r)
	}
}

// Commands pipelined behind STLS were sent in plaintext; the server must not
// carry them into the encrypted session.
func TestSTLSRejectsInjectedCommands(t *testing.T) {
	_, plain, _, _ := startTLS(t)
	c := dial(t, plain)
	c.send("STLS\r\nUSER injected@test.local")
	if r := c.line(); r != "-ERR unexpected data after STLS" {
		t.Fatalf("STLS with trailing data = %q", r)
	}
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.r.ReadString('\n'); err != io.EOF {
		t.Errorf("connection still open: %v", err)
	}
}

func TestPOP3S(t *testing.T) {
	st, _, implicit, clientTLS := startTLS(t)
	deliver(t, st, "dev@test.local", "Hi", "x\r\n")
	conn, err := tls.Dial("tcp", implicit, clientTLS)
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &client{t: t, conn: conn, r: bufio.NewReader(conn)}
	if c.banner = c.line(); !strings.HasPrefix(c.banner, "+OK Envry POP3") {
		t.Fatalf("greeting = %q", c.banner)
	}
	if has(capa(t, c), "STLS") {
		t.Error("STLS advertised on the POP3S port")
	}
	c.login("dev@test.local")
	if r := c.cmd("STAT"); !strings.HasPrefix(r, "+OK 1 ") {
		t.Errorf("STAT = %q", r)
	}
}
