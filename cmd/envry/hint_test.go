package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/arifurrahmanns/envry/internal/app"
	"github.com/arifurrahmanns/envry/internal/config"
)

func TestHintPortInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	cfg := &config.Config{
		SMTPAddr: busy.Addr().String(), POP3Addr: "127.0.0.1:0", IMAPAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
		DBPath: ":memory:", MaxMessages: 10, MaxSize: 1 << 20,
	}
	a, err := app.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Store().Close()

	err = a.Listen()
	var le *app.ListenError
	if !errors.As(err, &le) || le.Service != "smtp" {
		t.Fatalf("Listen error = %v, want a ListenError for smtp", err)
	}
	_, port, _ := net.SplitHostPort(busy.Addr().String())
	h := hint(err)
	for _, want := range []string{"Port " + port + " (SMTP) is already in use", "Mailpit", "envry --smtp :2525"} {
		if !strings.Contains(h, want) {
			t.Errorf("hint %q does not mention %q", h, want)
		}
	}
}

func TestHintIgnoresOtherErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("something else"),
		&app.ListenError{Service: "http", Addr: "bad:addr:1", Err: errors.New("missing port")},
	} {
		if h := hint(err); h != "" {
			t.Errorf("hint(%v) = %q, want none", err, h)
		}
	}
}

func TestWelcome(t *testing.T) {
	var b bytes.Buffer
	welcome(&b, app.Addrs{HTTP: "[::]:8025", SMTP: "0.0.0.0:1025", POP3: "127.0.0.1:1110", IMAP: ":1143"})
	out := b.String()
	for _, want := range []string{"is running", "http://localhost:8025", "localhost:1025", "127.0.0.1:1110", "localhost:1143", "Close this window"} {
		if !strings.Contains(out, want) {
			t.Errorf("welcome output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SMTPS") {
		t.Errorf("TLS rows shown without TLS:\n%s", out)
	}
}

// Cobra exits with "This is a command line tool" when a program is started
// from Explorer unless its mousetrap text is cleared; double-click and the
// Start-menu shortcut depend on it being off.
func TestStartsFromExplorer(t *testing.T) {
	newRootCmd()
	if cobra.MousetrapHelpText != "" {
		t.Errorf("cobra.MousetrapHelpText = %q; Envry would refuse to start from Explorer", cobra.MousetrapHelpText)
	}
}

func TestLocalAddr(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8025":   "localhost:8025",
		"[::]:8025":      "localhost:8025",
		":8025":          "localhost:8025",
		"127.0.0.1:8025": "127.0.0.1:8025",
		"mail.test:8025": "mail.test:8025",
	}
	for in, want := range cases {
		if got := localAddr(in); got != want {
			t.Errorf("localAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
