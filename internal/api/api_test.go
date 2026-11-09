package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/emulate"
	"github.com/arifurrahmanns/envry/internal/store"
)

type env struct {
	t     *testing.T
	store *store.Store
	srv   *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SMTPAddr: "127.0.0.1:2525", POP3Addr: ":1110", IMAPAddr: "[::1]:1143", HTTPAddr: "0.0.0.0:8025",
		MaxMessages: 1000, MaxSize: 25 << 20,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(cfg, st, log)
	a.SetWebhooks(emulate.New(st, log, 0).Webhooks())
	r := chi.NewRouter()
	r.Mount("/api", a.Routes())
	r.Get("/ws", a.HandleWS)
	srv := httptest.NewServer(r)
	t.Cleanup(func() {
		a.Close()
		srv.Close()
		st.Close()
	})
	return &env{t: t, store: st, srv: srv}
}

func (e *env) do(method, path string, body any) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func (e *env) json(method, path string, body any, wantStatus int, out any) {
	e.t.Helper()
	res, b := e.do(method, path, body)
	if res.StatusCode != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.StatusCode, wantStatus, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			e.t.Fatalf("%s %s: decode %s: %v", method, path, b, err)
		}
	}
}

const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

var richMessage = "From: Ava <ava@northwind.test>\r\n" +
	"To: Dev <dev@test.local>\r\n" +
	"Subject: Invoice ready\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=m\r\n\r\n" +
	"--m\r\nContent-Type: multipart/related; boundary=r\r\n\r\n" +
	"--r\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	`<html><head><title>x</title></head><body><img src="cid:logo"><script>alert(1)</script><a href="https://example.test">x</a></body></html>` + "\r\n" +
	"--r\r\nContent-Type: image/png\r\nContent-ID: <logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n" + pixel + "\r\n--r--\r\n" +
	"--m\r\nContent-Type: text/html; name=page.html\r\nContent-Disposition: attachment; filename=page.html\r\n\r\n<script>alert(2)</script>\r\n--m--\r\n"

func (e *env) ingest(raw string, rcpts ...string) string {
	e.t.Helper()
	sum, err := e.store.Ingest(context.Background(), store.IngestInput{
		EnvelopeFrom: "app@example.test", Recipients: rcpts, Raw: []byte(raw),
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return sum.ID
}

func TestInfo(t *testing.T) {
	e := newEnv(t)
	e.ingest("Subject: hi\r\n\r\nbody", "dev@test.local")
	var info infoDTO
	e.json("GET", "/api/v1/info", nil, 200, &info)
	want := Ports{SMTP: 2525, POP3: 1110, IMAP: 1143, HTTP: 8025}
	if info.Ports != want || info.Version == "" || info.Stats.Messages != 1 || info.Settings.MaxMessages != 1000 {
		t.Errorf("info = %+v", info)
	}
}

func TestMessages(t *testing.T) {
	e := newEnv(t)
	id := e.ingest(richMessage, "dev@test.local", "audit@test.local")
	e.ingest("From: x@y.test\r\nSubject: Other\r\n\r\nhello", "qa@test.local")

	var page store.Page[store.MessageSummary]
	e.json("GET", "/api/v1/messages", nil, 200, &page)
	if page.Total != 2 || len(page.Items) != 2 || page.Items[1].ID != id {
		t.Fatalf("list = %+v", page)
	}
	e.json("GET", "/api/v1/messages?q=invoice", nil, 200, &page)
	if page.Total != 1 || page.Items[0].ID != id {
		t.Errorf("search = %+v", page)
	}
	e.json("GET", "/api/v1/messages?limit=1", nil, 200, &page)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page 1 = %+v", page)
	}
	e.json("GET", "/api/v1/messages?limit=1&before="+*page.NextCursor, nil, 200, &page)
	if len(page.Items) != 1 || page.Items[0].ID != id || page.NextCursor != nil {
		t.Errorf("page 2 = %+v", page)
	}

	var d store.MessageDetail
	e.json("GET", "/api/v1/messages/"+id, nil, 200, &d)
	if d.Subject != "Invoice ready" || len(d.Attachments) != 2 || len(d.Recipients) != 2 || d.Recipients[1].Type != "bcc" {
		t.Errorf("detail = %+v", d)
	}
	e.json("GET", "/api/v1/messages/nope", nil, 404, nil)

	res, body := e.do("GET", "/api/v1/messages/"+id+"/raw", nil)
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "From: Ava") || res.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("raw: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	res, _ = e.do("GET", "/api/v1/messages/"+id+"/raw?download=1", nil)
	if res.Header.Get("Content-Type") != "message/rfc822" || !strings.Contains(res.Header.Get("Content-Disposition"), id+".eml") {
		t.Errorf("raw download headers: %v", res.Header)
	}

	// The HTML attachment downloads with a sandbox policy instead of rendering.
	var htmlAttachment string
	for _, a := range d.Attachments {
		if a.Filename == "page.html" {
			htmlAttachment = a.ID
		}
	}
	res, body = e.do("GET", "/api/v1/messages/"+id+"/attachments/"+htmlAttachment, nil)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") ||
		res.Header.Get("Content-Security-Policy") != "sandbox" || !strings.Contains(string(body), "alert(2)") {
		t.Errorf("attachment: %d %v", res.StatusCode, res.Header)
	}
	e.json("GET", "/api/v1/messages/"+id+"/attachments/99", nil, 404, nil)

	e.json("DELETE", "/api/v1/messages/"+id, nil, 204, nil)
	e.json("DELETE", "/api/v1/messages/"+id, nil, 404, nil)
	e.json("DELETE", "/api/v1/messages", nil, 204, nil)
	e.json("GET", "/api/v1/messages", nil, 200, &page)
	if page.Total != 0 || page.Items == nil {
		t.Errorf("after clear = %+v", page)
	}
}

func TestHTMLPreview(t *testing.T) {
	e := newEnv(t)
	id := e.ingest(richMessage, "dev@test.local")

	res, body := e.do("GET", "/api/v1/messages/"+id+"/html", nil)
	html := string(body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status %d type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if !strings.Contains(html, `src="data:image/png;base64,`+pixel[:20]) || strings.Contains(html, "cid:logo") {
		t.Errorf("cid image not inlined: %s", html)
	}
	if !strings.Contains(html, `<head><base target="_blank">`) {
		t.Errorf("base target missing: %s", html)
	}
	csp := res.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "img-src data:;", "sandbox allow-popups", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src") || strings.Contains(csp, "allow-scripts") || strings.Contains(csp, "allow-same-origin") {
		t.Errorf("CSP allows scripts or same-origin: %q", csp)
	}

	res, _ = e.do("GET", "/api/v1/messages/"+id+"/html?remote=1", nil)
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "img-src data: http: https:") {
		t.Errorf("remote CSP = %q", res.Header.Get("Content-Security-Policy"))
	}

	textOnly := e.ingest("Subject: plain\r\n\r\njust text", "dev@test.local")
	e.json("GET", "/api/v1/messages/"+textOnly+"/html", nil, 404, nil)
}

func TestMailboxesAccountsAndDeliveries(t *testing.T) {
	e := newEnv(t)
	e.ingest("From: a@x.test\r\nTo: dev@test.local\r\nSubject: one\r\n\r\nx", "dev@test.local", "qa@test.local")

	var list struct{ Items []store.Mailbox }
	e.json("GET", "/api/v1/mailboxes", nil, 200, &list)
	if len(list.Items) != 2 {
		t.Fatalf("mailboxes = %+v", list)
	}
	var mb store.Mailbox
	e.json("GET", "/api/v1/mailboxes/QA@test.local", nil, 200, &mb)
	if mb.Address != "qa@test.local" || mb.Unread != 1 {
		t.Errorf("mailbox = %+v", mb)
	}
	e.json("GET", "/api/v1/mailboxes/nobody@test.local/messages", nil, 404, nil)

	// The UI percent-encodes addresses in paths.
	e.json("GET", "/api/v1/mailboxes/qa%40test.local", nil, 200, &mb)
	var page store.Page[store.Delivery]
	e.json("GET", "/api/v1/mailboxes/qa%40test.local/messages?folder=INBOX", nil, 200, &page)
	if len(page.Items) != 1 || page.Items[0].RcptType != "bcc" || page.Items[0].Seen {
		t.Fatalf("qa inbox = %+v", page)
	}
	did := page.Items[0].DeliveryID

	var d store.Delivery
	e.json("PATCH", fmt.Sprintf("/api/v1/deliveries/%d", did), map[string]any{"seen": true}, 200, &d)
	if !d.Seen || d.Flagged {
		t.Errorf("patched = %+v", d)
	}
	e.json("PATCH", fmt.Sprintf("/api/v1/deliveries/%d", did), map[string]any{"bogus": 1}, 400, nil)
	e.json("PATCH", "/api/v1/deliveries/9999", map[string]any{"seen": true}, 404, nil)
	e.json("PATCH", "/api/v1/deliveries/abc", map[string]any{"seen": true}, 404, nil)
	e.json("DELETE", fmt.Sprintf("/api/v1/deliveries/%d", did), nil, 204, nil)
	e.json("GET", "/api/v1/mailboxes/qa@test.local/messages", nil, 200, &page)
	if page.Total != 0 {
		t.Errorf("after delete = %+v", page)
	}

	e.json("POST", "/api/v1/accounts", map[string]any{"address": "Me@Test.local", "displayName": "Me"}, 201, &mb)
	if !mb.IsAccount || mb.Address != "me@test.local" || len(mb.Folders) != 4 {
		t.Errorf("created account = %+v", mb)
	}
	e.json("POST", "/api/v1/accounts", map[string]any{"address": "me@test.local"}, 200, nil)
	e.json("POST", "/api/v1/accounts", map[string]any{"address": "not an address"}, 400, nil)
	e.json("GET", "/api/v1/accounts/me%40test.local", nil, 200, &mb)
	e.json("GET", "/api/v1/accounts/dev@test.local", nil, 404, nil)
	e.json("DELETE", "/api/v1/accounts/me@test.local", nil, 204, nil)
	e.json("GET", "/api/v1/accounts/me@test.local", nil, 404, nil)
}

func TestSend(t *testing.T) {
	e := newEnv(t)
	var out sendResponse
	e.json("POST", "/api/v1/send", map[string]any{
		"from": "Me <me@test.local>", "to": []string{"dev@test.local"}, "bcc": []string{"secret@test.local"},
		"subject": "Grüße", "text": "Hallo\nWelt", "html": "<p>Hallo</p>",
	}, 201, &out)

	var d store.MessageDetail
	e.json("GET", "/api/v1/messages/"+out.ID, nil, 200, &d)
	if d.Subject != "Grüße" || d.Source != store.SourceAPI || !d.HasHTML || d.Text == nil || !strings.Contains(*d.Text, "Welt") {
		t.Errorf("sent message = %+v", d)
	}
	raw, _ := e.store.Raw(context.Background(), out.ID)
	if strings.Contains(string(raw), "secret@test.local") {
		t.Error("Bcc address leaked into the message headers")
	}
	if len(d.Recipients) != 2 || d.Recipients[1] != (store.Recipient{Address: "secret@test.local", Type: "bcc"}) {
		t.Errorf("recipients = %+v", d.Recipients)
	}

	e.json("POST", "/api/v1/send", map[string]any{"to": []string{}}, 400, nil)
	e.json("POST", "/api/v1/send", map[string]any{"to": []string{"bad address"}}, 400, nil)

	// Non-JSON bodies are refused, so cross-site form posts cannot send mail.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/send", strings.NewReader(`{"to":["dev@test.local"]}`))
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST status = %d", res.StatusCode)
	}
}

func TestWait(t *testing.T) {
	e := newEnv(t)

	// Already-received mail matches immediately.
	e.ingest("From: a@x.test\r\nTo: dev@test.local\r\nSubject: Welcome aboard\r\n\r\nhi", "dev@test.local")
	var d store.MessageDetail
	e.json("GET", "/api/v1/wait?to=dev@test.local&subject=welcome&timeout=1s", nil, 200, &d)
	if d.Subject != "Welcome aboard" {
		t.Errorf("wait = %+v", d.MessageSummary)
	}

	// Mail that arrives while waiting returns as soon as it lands.
	go func() {
		time.Sleep(150 * time.Millisecond)
		e.ingest("From: a@x.test\r\nTo: qa@test.local\r\nSubject: Reset your password\r\n\r\nlink", "qa@test.local")
	}()
	start := time.Now()
	e.json("GET", "/api/v1/wait?to=qa@test.local&subject=Reset&timeout=5s", nil, 200, &d)
	if d.Subject != "Reset your password" || time.Since(start) > 2*time.Second {
		t.Errorf("wait for new mail = %q after %v", d.Subject, time.Since(start))
	}

	e.json("GET", "/api/v1/wait?to=nobody@test.local&timeout=200ms", nil, 408, nil)
	e.json("GET", "/api/v1/wait?timeout=soon", nil, 400, nil)
	e.json("GET", "/api/v1/wait?since=yesterday", nil, 400, nil)
	e.json("GET", "/api/v1/wait?to=dev@test.local&since="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+"&timeout=100ms", nil, 408, nil)
}

func TestWebSocketEvents(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	start := time.Now()
	id := e.ingest("From: a@x.test\r\nTo: dev@test.local\r\nSubject: Live\r\n\r\nhi", "dev@test.local")
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ev struct {
			Type string
			Data store.MessageSummary
		}
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != "message.created" {
			continue
		}
		latency := time.Since(start)
		if ev.Data.ID != id || ev.Data.Subject != "Live" {
			t.Errorf("event = %+v", ev)
		}
		if latency > 500*time.Millisecond {
			t.Errorf("message.created took %v", latency)
		}
		t.Logf("message.created delivered in %v", latency)
		return
	}
}

func TestWebSocketRejectsOtherOrigins(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin dial: err=%v status=%v", err, res)
	}
}

func TestUnknownRoutesAreJSON(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/v1/nope", "/api/v2/info"} {
		res, _ := e.do("GET", p, nil)
		if res.StatusCode != 404 || res.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s: %d %q", p, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	res, _ := e.do("PUT", "/api/v1/info", map[string]any{})
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT /info = %d", res.StatusCode)
	}
}

func TestChecks(t *testing.T) {
	e := newEnv(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
		}
	}))
	defer site.Close()

	id := e.ingest("From: app@example.test\r\nTo: dev@test.local\r\nSubject: Welcome\r\n"+
		"Date: Mon, 2 Nov 2026 10:00:00 +0000\r\nMessage-ID: <1@example.test>\r\n"+
		"Content-Type: text/html; charset=utf-8\r\n\r\n"+
		`<p><a href="`+site.URL+`/ok">Home</a> <a href="`+site.URL+`/missing">Docs</a></p>`+"\r\n", "dev@test.local")

	var report struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
		Links []struct {
			URL string `json:"url"`
		} `json:"links"`
	}
	e.json("GET", "/api/v1/messages/"+id+"/checks", nil, 200, &report)
	statuses := map[string]string{}
	for _, c := range report.Checks {
		statuses[c.ID] = c.Status
	}
	if statuses["text-part"] != "fail" || statuses["headers"] != "pass" || len(report.Links) != 2 {
		t.Errorf("report = %+v", report)
	}

	var links linkResultsDTO
	e.json("POST", "/api/v1/messages/"+id+"/checks/links", map[string]any{}, 200, &links)
	if links.Total != 2 || len(links.Results) != 2 ||
		!links.Results[0].OK || links.Results[0].Status != 200 ||
		links.Results[1].OK || links.Results[1].Status != 404 {
		t.Errorf("link results = %+v", links)
	}

	e.json("GET", "/api/v1/messages/nope/checks", nil, 404, nil)
	e.json("POST", "/api/v1/messages/nope/checks/links", map[string]any{}, 404, nil)
}

func TestExtracts(t *testing.T) {
	e := newEnv(t)
	id := e.ingest("From: app@example.test\r\nTo: dev@test.local\r\nSubject: 482913 is your verification code\r\n"+
		"Content-Type: text/html; charset=utf-8\r\n\r\n"+
		`<p>Enter this code:</p><p>482913</p>`+
		`<p><a href="http://localhost:3000/verify?token=8f2c5e1d7a9b3c4f6e0d2a1b">Verify email</a></p>`+"\r\n", "dev@test.local")

	var got struct {
		Codes []struct {
			Value   string `json:"value"`
			Context string `json:"context"`
		} `json:"codes"`
		Links []struct {
			URL  string `json:"url"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"links"`
	}
	e.json("GET", "/api/v1/messages/"+id+"/extracts", nil, 200, &got)
	if len(got.Codes) != 1 || got.Codes[0].Value != "482913" || got.Codes[0].Context == "" {
		t.Errorf("codes = %+v", got.Codes)
	}
	if len(got.Links) != 1 || got.Links[0].Kind != "verify" || got.Links[0].Text != "Verify email" ||
		got.Links[0].URL != "http://localhost:3000/verify?token=8f2c5e1d7a9b3c4f6e0d2a1b" {
		t.Errorf("links = %+v", got.Links)
	}

	// Nothing found is two empty lists, not null.
	plain := e.ingest("From: a@example.test\r\nTo: dev@test.local\r\nSubject: Hi\r\n\r\nHello there.\r\n", "dev@test.local")
	_, b := e.do("GET", "/api/v1/messages/"+plain+"/extracts", nil)
	if strings.TrimSpace(string(b)) != `{"codes":[],"links":[]}` {
		t.Errorf("empty extracts = %s", b)
	}
	e.json("GET", "/api/v1/messages/nope/extracts", nil, 404, nil)
}

func TestInfoFailureSimulation(t *testing.T) {
	e := newEnv(t)
	_, b := e.do("GET", "/api/v1/info", nil)
	if !strings.Contains(string(b), `"smtpRejectRcpt":[]`) || !strings.Contains(string(b), `"smtpRejectRate":0`) {
		t.Errorf("info = %s", b)
	}
}

func TestWebhookSettings(t *testing.T) {
	e := newEnv(t)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer app.Close()

	var list struct {
		Items []struct {
			Provider        string `json:"provider"`
			Signing         string `json:"signing"`
			Secret          string `json:"secret"`
			VerificationKey string `json:"verificationKey"`
		} `json:"items"`
	}
	e.json("GET", "/api/v1/webhooks", nil, 200, &list)
	if len(list.Items) != 5 || list.Items[0].Provider != "resend" || list.Items[0].Signing != "svix" ||
		list.Items[1].VerificationKey == "" || list.Items[3].Secret == "" {
		t.Fatalf("list = %+v", list)
	}

	var saved struct {
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
	}
	e.json("PUT", "/api/v1/webhooks/postmark", map[string]any{"url": app.URL, "enabled": true}, 200, &saved)
	if saved.URL != app.URL || !saved.Enabled {
		t.Errorf("saved = %+v", saved)
	}
	var d struct {
		Status int    `json:"status"`
		Event  string `json:"event"`
	}
	e.json("POST", "/api/v1/webhooks/postmark/test", map[string]any{}, 200, &d)
	if d.Status != 204 || d.Event != "delivered" {
		t.Errorf("test delivery = %+v", d)
	}
	var log struct {
		Items []struct {
			Provider string `json:"provider"`
		} `json:"items"`
	}
	e.json("GET", "/api/v1/webhooks/deliveries", nil, 200, &log)
	if len(log.Items) != 1 || log.Items[0].Provider != "postmark" {
		t.Errorf("log = %+v", log)
	}

	e.json("PUT", "/api/v1/webhooks/sparkpost", map[string]any{"url": app.URL}, 404, nil)
	e.json("PUT", "/api/v1/webhooks/resend", map[string]any{"url": "not a url", "enabled": true}, 400, nil)
}
