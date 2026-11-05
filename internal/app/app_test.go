package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/config"
)

func start(t *testing.T, cfg *config.Config) Addrs {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})
	return a.Addrs()
}

func testConfig() *config.Config {
	return &config.Config{
		SMTPAddr: "127.0.0.1:0", POP3Addr: "127.0.0.1:1110",
		IMAPAddr: "127.0.0.1:1143", HTTPAddr: "127.0.0.1:0",
		DBPath: ":memory:", MaxMessages: 1000, MaxSize: 1 << 20,
	}
}

func get(t *testing.T, url string, mod func(*http.Request)) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if mod != nil {
		mod(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestSMTPToAPI(t *testing.T) {
	cfg := testConfig()
	cfg.Accounts = []config.Account{{Address: "dev@test.local"}}
	addrs := start(t, cfg)
	base := "http://" + addrs.HTTP

	msg := "From: App <app@example.test>\r\nTo: dev@test.local\r\nSubject: Forgot password\r\n\r\nReset: http://localhost/reset?t=1\r\n"
	if err := smtp.SendMail(addrs.SMTP, nil, "app@example.test", []string{"dev@test.local", "audit@test.local"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}

	res, body := get(t, base+"/api/v1/wait?to=dev@test.local&subject=forgot&timeout=5s", nil)
	if res.StatusCode != 200 || !strings.Contains(body, "reset?t=1") {
		t.Fatalf("wait: %d %s", res.StatusCode, body)
	}

	_, body = get(t, base+"/api/v1/mailboxes", nil)
	var list struct {
		Items []struct {
			Address   string
			IsAccount bool
			Total     int
		}
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || list.Items[1].Address != "dev@test.local" || !list.Items[1].IsAccount {
		t.Errorf("mailboxes = %+v", list.Items)
	}

	for _, p := range []string{"/", "/sent", "/receivers/dev@test.local"} {
		res, body := get(t, base+p, nil)
		if res.StatusCode != 200 || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
			t.Errorf("%s: %d %.60q", p, res.StatusCode, body)
		}
	}
	if res, _ := get(t, base+"/api/nope", nil); res.StatusCode != 404 {
		t.Errorf("/api/nope: %d", res.StatusCode)
	}
}

func TestUIAuth(t *testing.T) {
	cfg := testConfig()
	cfg.UIAuth = &config.BasicAuth{User: "admin", Password: "s3cret"}
	base := "http://" + start(t, cfg).HTTP

	for _, p := range []string{"/", "/api/v1/info", "/ws"} {
		res, _ := get(t, base+p, nil)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("%s without creds: %d", p, res.StatusCode)
		}
		res, _ = get(t, base+p, func(r *http.Request) { r.SetBasicAuth("admin", "wrong") })
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s wrong creds: %d", p, res.StatusCode)
		}
	}
	for _, p := range []string{"/", "/api/v1/info"} {
		res, _ := get(t, base+p, func(r *http.Request) { r.SetBasicAuth("admin", "s3cret") })
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s good creds: %d", p, res.StatusCode)
		}
	}
}

func TestStartupFailsOnBusyPort(t *testing.T) {
	first := start(t, testConfig())
	cfg := testConfig()
	cfg.SMTPAddr = first.SMTP
	a, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "listen smtp") {
		t.Errorf("Run on busy port: %v", err)
	}
}
