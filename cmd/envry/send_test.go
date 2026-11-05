package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/app"
	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/store"
)

func startApp(t *testing.T, cfg *config.Config) *app.App {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Listen(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})
	return a
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSendCommand(t *testing.T) {
	cfg := &config.Config{
		SMTPAddr: "127.0.0.1:0", POP3Addr: "127.0.0.1:0", IMAPAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
		DBPath: ":memory:", MaxMessages: 100, MaxSize: 1 << 20,
	}
	a := startApp(t, cfg)

	attachment := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(attachment, []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "send", "--smtp", a.Addrs().SMTP, "--to", "dev@test.local", "--bcc", "audit@test.local",
		"--subject", "From the CLI", "--html", "<p>Hello</p>", "--attach", attachment)
	if err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Sent "From the CLI" to 2 recipients`) {
		t.Errorf("output = %q", out)
	}

	ctx := context.Background()
	page, _ := a.Store().ListMessages(ctx, store.ListOptions{})
	if page.Total != 1 {
		t.Fatalf("stored %d messages", page.Total)
	}
	d, _ := a.Store().GetMessage(ctx, page.Items[0].ID)
	if d.Subject != "From the CLI" || !d.HasHTML || len(d.Attachments) != 1 || len(d.Recipients) != 2 {
		t.Errorf("stored message = %+v", d)
	}
}

func TestSendRequiresRecipient(t *testing.T) {
	if _, err := run(t, "send", "--smtp", "127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("err = %v", err)
	}
}

func TestDialAddr(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:1025":   "127.0.0.1:1025",
		":1025":          "127.0.0.1:1025",
		"[::]:1025":      "127.0.0.1:1025",
		"127.0.0.1:2525": "127.0.0.1:2525",
		"mail.test:25":   "mail.test:25",
	}
	for in, want := range cases {
		if got := dialAddr(in); got != want {
			t.Errorf("dialAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
