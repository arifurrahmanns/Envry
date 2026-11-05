package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arifurrahmanns/envry/internal/config"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestInfo(t *testing.T) {
	cfg := &config.Config{
		SMTPAddr: "127.0.0.1:2525", POP3Addr: ":1110", IMAPAddr: "[::1]:1143", HTTPAddr: "0.0.0.0:8025",
	}
	srv := httptest.NewServer(New(cfg, discard()).Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var got infoDTO
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := portsDTO{SMTP: 2525, POP3: 1110, IMAP: 1143, HTTP: 8025}
	if got.Ports != want || got.Version == "" {
		t.Errorf("got %+v, want ports %+v", got, want)
	}
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	srv := httptest.NewServer(New(&config.Config{}, discard()).Routes())
	defer srv.Close()
	for _, p := range []string{"/v1/nope", "/v2/info"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s: status %d, type %q", p, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	res, err := http.Post(srv.URL+"/v1/info", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/info: status %d", res.StatusCode)
	}
}
