package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/config"
)

func start(t *testing.T, cfg *config.Config) string {
	t.Helper()
	a := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := a.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})
	return "http://" + a.Addrs().HTTP
}

func testConfig() *config.Config {
	return &config.Config{
		SMTPAddr: "127.0.0.1:1025", POP3Addr: "127.0.0.1:1110",
		IMAPAddr: "127.0.0.1:1143", HTTPAddr: "127.0.0.1:0",
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

func TestServesAPIAndUI(t *testing.T) {
	base := start(t, testConfig())

	res, body := get(t, base+"/api/v1/info", nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"smtp":1025`) {
		t.Errorf("/api/v1/info: %d %s", res.StatusCode, body)
	}
	res, body = get(t, base+"/api/nope", nil)
	if res.StatusCode != 404 || !strings.Contains(body, `"error"`) {
		t.Errorf("/api/nope: %d %s", res.StatusCode, body)
	}
	for _, p := range []string{"/", "/sent", "/receivers/a@b.test"} {
		res, body = get(t, base+p, nil)
		if res.StatusCode != 200 || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
			t.Errorf("%s: %d %.80q", p, res.StatusCode, body)
		}
	}
}

func TestUIAuth(t *testing.T) {
	cfg := testConfig()
	cfg.UIAuth = &config.BasicAuth{User: "admin", Password: "s3cret"}
	base := start(t, cfg)

	for _, p := range []string{"/", "/api/v1/info"} {
		res, _ := get(t, base+p, nil)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("%s without creds: %d", p, res.StatusCode)
		}
		res, _ = get(t, base+p, func(r *http.Request) { r.SetBasicAuth("admin", "wrong") })
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s wrong creds: %d", p, res.StatusCode)
		}
		res, _ = get(t, base+p, func(r *http.Request) { r.SetBasicAuth("admin", "s3cret") })
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s good creds: %d", p, res.StatusCode)
		}
	}
}
