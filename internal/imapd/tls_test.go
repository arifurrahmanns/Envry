package imapd

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/arifurrahmanns/envry/internal/store"
	"github.com/arifurrahmanns/envry/internal/tlsutil"
)

func TestStartTLSAndIMAPS(t *testing.T) {
	cert, err := tlsutil.Load(tlsutil.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), store.Options{Path: ":memory:"})
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		for range 2 {
			if err := <-done; err != nil {
				t.Errorf("Serve: %v", err)
			}
		}
		st.Close()
	})
	e := &env{t: t, store: st}
	e.deliver("dev@test.local", "Secret", "encrypted body")

	opts := &imapclient.Options{TLSConfig: &tls.Config{RootCAs: cert.CertPool(), ServerName: "localhost"}}

	plain, err := imapclient.DialInsecure(ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.WaitGreeting(); err != nil {
		t.Fatal(err)
	}
	if !plain.Caps().Has(imap.CapStartTLS) {
		t.Error("STARTTLS not advertised on the plain port")
	}
	plain.Close()

	for name, dial := range map[string]func() (*imapclient.Client, error){
		"STARTTLS": func() (*imapclient.Client, error) { return imapclient.DialStartTLS(ln.Addr().String(), opts) },
		"IMAPS":    func() (*imapclient.Client, error) { return imapclient.DialTLS(tlsLn.Addr().String(), opts) },
	} {
		c, err := dial()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.Caps().Has(imap.CapStartTLS) {
			t.Errorf("%s: STARTTLS advertised on an encrypted connection", name)
		}
		if err := c.Login("dev@test.local", "x").Wait(); err != nil {
			t.Fatalf("%s login: %v", name, err)
		}
		data, err := c.Select("INBOX", nil).Wait()
		if err != nil || data.NumMessages != 1 {
			t.Errorf("%s SELECT = %+v, %v", name, data, err)
		}
		c.Close()
	}
}
