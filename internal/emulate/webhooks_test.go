package emulate

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/store"
)

// hook is one call the app's webhook endpoint received.
type hook struct {
	header http.Header
	body   []byte
}

// webhookEnv is an emulator plus a stand-in for the app's webhook endpoint.
type webhookEnv struct {
	*env
	server *Server
	calls  chan hook
	url    string
}

func newWebhookEnv(t *testing.T) *webhookEnv {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
	s.webhooks.delay = 10 * time.Millisecond
	r := chi.NewRouter()
	r.Mount("/emulate", s.Routes())
	srv := httptest.NewServer(r)

	calls := make(chan hook, 32)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls <- hook{r.Header.Clone(), b}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(func() {
		app.Close()
		srv.Close()
		time.Sleep(50 * time.Millisecond) // let late events finish before the store closes
		st.Close()
	})
	return &webhookEnv{env: &env{t: t, store: st, srv: srv}, server: s, calls: calls, url: app.URL + "/webhooks"}
}

func (e *webhookEnv) enable(provider string) WebhookConfig {
	e.t.Helper()
	c, err := e.server.Webhooks().Save(context.Background(), provider, e.url, true, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// next waits for n calls.
func (e *webhookEnv) next(n int) []hook {
	e.t.Helper()
	var out []hook
	for len(out) < n {
		select {
		case h := <-e.calls:
			out = append(out, h)
		case <-time.After(3 * time.Second):
			e.t.Fatalf("got %d webhook calls, want %d", len(out), n)
		}
	}
	return out
}

func decode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
}

func TestOutcome(t *testing.T) {
	for addr, want := range map[string]string{
		"ada@test.local": EventDelivered, "bounce@test.local": EventBounced, "Bounce+42@x.test": EventBounced,
		"complaint@test.local": EventComplained, "complaint+a@x.test": EventComplained, "bouncer@x.test": EventDelivered,
	} {
		if got := outcome(addr); got != want {
			t.Errorf("outcome(%q) = %s, want %s", addr, got, want)
		}
	}
}

func TestResendWebhooks(t *testing.T) {
	e := newWebhookEnv(t)
	cfg := e.enable(Resend)
	if !strings.HasPrefix(cfg.Secret, "whsec_") || cfg.Signing != "svix" {
		t.Fatalf("config = %+v", cfg)
	}
	_, body := e.post("/emulate/resend/emails", "application/json", map[string]any{
		"from": "Acme <hi@acme.test>", "to": []string{"ada@test.local", "bounce@test.local"}, "subject": "Welcome",
		"text": "Hi", "tags": []map[string]string{{"name": "flow", "value": "signup"}},
	})
	var sent struct{ ID string }
	decode(t, body, &sent)

	types := map[string]string{}
	for _, h := range e.next(2) {
		// Verify like the Svix library: HMAC of id.timestamp.body with the
		// base64-decoded secret.
		key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(cfg.Secret, "whsec_"))
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(h.header.Get("Svix-Id") + "." + h.header.Get("Svix-Timestamp") + "." + string(h.body)))
		if want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)); h.header.Get("Svix-Signature") != want {
			t.Errorf("signature %q, want %q", h.header.Get("Svix-Signature"), want)
		}
		var ev struct {
			Type string
			Data struct {
				EmailID string `json:"email_id"`
				To      []string
				Tags    map[string]string
				Bounce  *struct{ Type string }
			}
		}
		decode(t, h.body, &ev)
		if ev.Data.EmailID != sent.ID || ev.Data.Tags["flow"] != "signup" {
			t.Errorf("event = %+v", ev)
		}
		types[ev.Data.To[0]] = ev.Type
		if ev.Type == "email.bounced" && (ev.Data.Bounce == nil || ev.Data.Bounce.Type != "Permanent") {
			t.Errorf("bounce = %s", h.body)
		}
	}
	if types["ada@test.local"] != "email.delivered" || types["bounce@test.local"] != "email.bounced" {
		t.Errorf("types = %v", types)
	}
}

func TestSendGridWebhooks(t *testing.T) {
	e := newWebhookEnv(t)
	cfg := e.enable(SendGrid)
	der, err := base64.StdEncoding.DecodeString(cfg.VerificationKey)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := e.post("/emulate/sendgrid/v3/mail/send", "application/json", map[string]any{
		"personalizations": []map[string]any{{
			"to":          []map[string]string{{"email": "ada@test.local"}, {"email": "complaint@test.local"}},
			"custom_args": map[string]string{"user_id": "42"},
		}},
		"from": map[string]string{"email": "hi@acme.test"}, "subject": "Hi",
		"content":    []map[string]string{{"type": "text/plain", "value": "Hi"}},
		"categories": []string{"onboarding"},
	})
	id := res.Header.Get("X-Message-Id")

	// One array for the delivered events, then one for the spam report.
	calls := e.next(2)
	for _, h := range calls {
		digest := sha256.Sum256(append([]byte(h.header.Get("X-Twilio-Email-Event-Webhook-Timestamp")), h.body...))
		sig, _ := base64.StdEncoding.DecodeString(h.header.Get("X-Twilio-Email-Event-Webhook-Signature"))
		if !ecdsa.VerifyASN1(pub.(*ecdsa.PublicKey), digest[:], sig) {
			t.Errorf("signature does not verify for %s", h.body)
		}
	}
	var delivered, spam []map[string]any
	decode(t, calls[0].body, &delivered)
	decode(t, calls[1].body, &spam)
	if len(delivered) != 2 || delivered[0]["event"] != "delivered" || delivered[0]["user_id"] != "42" ||
		!strings.HasPrefix(delivered[0]["sg_message_id"].(string), id+".") {
		t.Errorf("delivered = %s", calls[0].body)
	}
	if cats, _ := delivered[0]["category"].([]any); len(cats) != 1 || cats[0] != "onboarding" {
		t.Errorf("category = %v", delivered[0]["category"])
	}
	if len(spam) != 1 || spam[0]["event"] != "spamreport" || spam[0]["email"] != "complaint@test.local" {
		t.Errorf("spam = %s", calls[1].body)
	}
}

func TestPostmarkWebhooks(t *testing.T) {
	e := newWebhookEnv(t)
	e.enable(Postmark)
	_, body := e.post("/emulate/postmark/email", "application/json", map[string]any{
		"From": "hi@acme.test", "To": "ada@test.local, bounce+1@test.local", "Subject": "Receipt", "TextBody": "Thanks",
		"Tag": "receipts", "Metadata": map[string]string{"order": "7"},
	})
	var sent pmResult
	decode(t, body, &sent)
	records := map[string]map[string]any{}
	for _, h := range e.next(2) {
		var rec map[string]any
		decode(t, h.body, &rec)
		records[rec["RecordType"].(string)] = rec
		if rec["MessageID"] != sent.MessageID || rec["Tag"] != "receipts" || rec["Metadata"].(map[string]any)["order"] != "7" {
			t.Errorf("record = %s", h.body)
		}
	}
	if records["Delivery"]["Recipient"] != "ada@test.local" || records["Bounce"]["Email"] != "bounce+1@test.local" ||
		records["Bounce"]["Type"] != "HardBounce" {
		t.Errorf("records = %v", records)
	}
}

func TestMailgunWebhooks(t *testing.T) {
	e := newWebhookEnv(t)
	cfg := e.enable(Mailgun)
	form := url.Values{
		"from": {"hi@mg.acme.test"}, "to": {"ada@test.local, bounce@test.local"}, "subject": {"Hi"}, "text": {"Hi"},
		"o:tag": {"welcome"}, "v:user-id": {"7"},
	}
	_, body := e.post("/emulate/mailgun/v3/mg.acme.test/messages", "application/x-www-form-urlencoded", form.Encode())
	var sent mgResult
	decode(t, body, &sent)
	events := map[string]string{}
	for _, h := range e.next(2) {
		var p struct {
			Signature struct{ Timestamp, Token, Signature string }
			Data      struct {
				Event         string
				Recipient     string
				Tags          []string
				UserVariables map[string]string `json:"user-variables"`
				Message       struct {
					Headers map[string]string
				}
			} `json:"event-data"`
		}
		decode(t, h.body, &p)
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write([]byte(p.Signature.Timestamp + p.Signature.Token))
		if hex.EncodeToString(mac.Sum(nil)) != p.Signature.Signature {
			t.Errorf("signature does not verify: %s", h.body)
		}
		if "<"+p.Data.Message.Headers["message-id"]+">" != sent.ID || p.Data.Tags[0] != "welcome" || p.Data.UserVariables["user-id"] != "7" {
			t.Errorf("event = %s", h.body)
		}
		events[p.Data.Recipient] = p.Data.Event
	}
	if events["ada@test.local"] != "delivered" || events["bounce@test.local"] != "failed" {
		t.Errorf("events = %v", events)
	}
}

func TestSESWebhooks(t *testing.T) {
	e := newWebhookEnv(t)
	e.enable(SES)
	_, body := e.post("/emulate/ses/v2/email/outbound-emails", "application/json", map[string]any{
		"FromEmailAddress": "hi@acme.test",
		"Destination":      map[string]any{"ToAddresses": []string{"ada@test.local", "complaint@test.local"}},
		"Content":          map[string]any{"Simple": map[string]any{"Subject": map[string]string{"Data": "Hi"}, "Body": map[string]any{"Text": map[string]string{"Data": "Hi"}}}},
		"EmailTags":        []map[string]string{{"Name": "flow", "Value": "signup"}},
	})
	var sent struct{ MessageId string }
	decode(t, body, &sent)
	var kinds []string
	for _, h := range e.next(3) {
		if h.header.Get("X-Amz-Sns-Message-Type") != "Notification" {
			t.Errorf("headers = %v", h.header)
		}
		var sns struct{ Type, Message, TopicArn string }
		decode(t, h.body, &sns)
		var ev struct {
			NotificationType string
			EventType        string
			Mail             struct {
				MessageID string `json:"messageId"`
				Tags      map[string][]string
			}
		}
		decode(t, []byte(sns.Message), &ev)
		if sns.Type != "Notification" || ev.Mail.MessageID != sent.MessageId || ev.Mail.Tags["flow"][0] != "signup" ||
			ev.NotificationType != ev.EventType {
			t.Errorf("notification = %s", h.body)
		}
		kinds = append(kinds, ev.NotificationType)
	}
	sort.Strings(kinds)
	if strings.Join(kinds, ",") != "Complaint,Delivery,Delivery" {
		t.Errorf("kinds = %v", kinds)
	}
}

func TestWebhookSettingsTestAndLog(t *testing.T) {
	e := newWebhookEnv(t)
	w := e.server.Webhooks()
	ctx := context.Background()

	// Nothing is sent while a provider's webhook is off.
	_, _ = e.post("/emulate/resend/emails", "application/json", map[string]any{"from": "a@acme.test", "to": "b@test.local", "text": "x"})
	select {
	case h := <-e.calls:
		t.Fatalf("unexpected call %s", h.body)
	case <-time.After(100 * time.Millisecond):
	}

	if _, err := w.Save(ctx, Resend, "", true, nil); err == nil {
		t.Error("enabling without a URL should fail")
	}
	if _, err := w.Save(ctx, Resend, "ftp://x", false, nil); err == nil {
		t.Error("a non-HTTP URL should fail")
	}
	bad := "not-a-secret"
	if _, err := w.Save(ctx, Resend, e.url, true, &bad); err == nil {
		t.Error("a malformed Resend secret should fail")
	}
	mine := "whsec_" + base64.StdEncoding.EncodeToString([]byte("my app's own secret"))
	if c, err := w.Save(ctx, Resend, e.url, false, &mine); err != nil || c.Secret != mine {
		t.Errorf("own secret: %+v %v", c, err)
	}

	d, err := w.SendTest(ctx, Resend)
	if err != nil || d.Status != 200 || d.Response != "ok" || d.Event != EventDelivered {
		t.Fatalf("test = %+v %v", d, err)
	}
	e.next(1)
	if log := w.Deliveries(); len(log) != 1 || log[0].ID != d.ID {
		t.Errorf("log = %+v", log)
	}

	configs, err := w.Configs(ctx)
	if err != nil || len(configs) != 5 || configs[1].VerificationKey == "" || configs[3].Secret == "" || configs[2].Signing != "none" {
		t.Errorf("configs = %+v %v", configs, err)
	}
}
