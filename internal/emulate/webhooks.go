package emulate

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/store"
)

// Delivery events: real providers call the app back when mail they sent
// is delivered, bounces or is reported as spam. For mail sent through an
// emulated API, Envry makes the same calls to the URL set for the
// provider: "delivered" for each recipient, "bounced" for bounce@...
// (or bounce+anything@...) and, after delivery, "complained" for
// complaint@.... Each call has the provider's format and signature.

// Event kinds.
const (
	EventDelivered  = "delivered"
	EventBounced    = "bounced"
	EventComplained = "complained"
)

// WebhookSent is the live event published after each call.
const WebhookSent = "webhook.sent"

// Providers lists the emulated providers in display order.
var Providers = []string{Resend, SendGrid, Postmark, Mailgun, SES}

// WebhookConfig is a provider's webhook settings as the UI shows them.
type WebhookConfig struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"`
	// Secret is what the app verifies requests with: Resend's signing
	// secret (whsec_...) or Mailgun's webhook signing key. Empty for the
	// others.
	Secret string `json:"secret"`
	// VerificationKey is SendGrid's public key (base64), for apps that
	// check SendGrid's signed event webhook.
	VerificationKey string `json:"verificationKey"`
	// Signing names the scheme: svix (Resend), ecdsa (SendGrid), hmac
	// (Mailgun), or none (Postmark and SES).
	Signing string `json:"signing"`
}

// Delivery is one webhook call, for the log.
type Delivery struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	Event      string    `json:"event"`
	MessageID  string    `json:"messageId"`
	Recipient  string    `json:"recipient"`
	URL        string    `json:"url"`
	Status     int       `json:"status"` // HTTP status; 0 when no answer came
	Error      string    `json:"error"`
	Response   string    `json:"response"` // the start of the answer's body
	DurationMs int64     `json:"durationMs"`
	SentAt     time.Time `json:"sentAt"`
}

const (
	maxDeliveries   = 100
	maxResponseText = 500
)

// Webhooks sends delivery events and keeps a log of recent calls.
type Webhooks struct {
	store  *store.Store
	log    *slog.Logger
	client *http.Client
	// delay is how long after sending the events arrive, as with a real
	// provider; it also lets the app finish handling its send first.
	delay time.Duration

	mu         sync.Mutex
	deliveries []Delivery // newest first
	nextID     int64
}

func newWebhooks(st *store.Store, log *slog.Logger) *Webhooks {
	return &Webhooks{
		store: st,
		log:   log,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Providers do not follow redirects either.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		delay: time.Second,
	}
}

// Webhooks returns the delivery event sender.
func (s *Server) Webhooks() *Webhooks { return s.webhooks }

// Configs returns every provider's settings, creating each provider's
// signing secret on first use.
func (w *Webhooks) Configs(ctx context.Context) ([]WebhookConfig, error) {
	out := make([]WebhookConfig, 0, len(Providers))
	for _, p := range Providers {
		c, err := w.Config(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Config returns one provider's settings.
func (w *Webhooks) Config(ctx context.Context, provider string) (WebhookConfig, error) {
	stored, err := w.settings(ctx, provider)
	if err != nil {
		return WebhookConfig{}, err
	}
	return view(stored)
}

// ErrUnknownProvider is returned for a provider Envry does not emulate.
var ErrUnknownProvider = errors.New("unknown provider")

// settings reads a provider's stored settings, saving a new secret the
// first time.
func (w *Webhooks) settings(ctx context.Context, provider string) (store.ProviderWebhook, error) {
	if !known(provider) {
		return store.ProviderWebhook{}, ErrUnknownProvider
	}
	st, found, err := w.store.ProviderWebhook(ctx, provider)
	if err != nil || (found && st.Secret != "") || !needsSecret(provider) {
		return st, err
	}
	if st.Secret, err = newSecret(provider); err != nil {
		return store.ProviderWebhook{}, err
	}
	return w.store.SaveProviderWebhook(ctx, st)
}

// Save sets a provider's URL and switch, and its secret when one is given
// (Resend and Mailgun accept the secret an app already uses).
func (w *Webhooks) Save(ctx context.Context, provider, rawURL string, enabled bool, secret *string) (WebhookConfig, error) {
	st, err := w.settings(ctx, provider)
	if err != nil {
		return WebhookConfig{}, err
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return WebhookConfig{}, invalid("The webhook URL must be an http:// or https:// address.")
		}
	}
	if enabled && rawURL == "" {
		return WebhookConfig{}, invalid("Enter the URL to send events to.")
	}
	st.URL, st.Enabled = rawURL, enabled
	if secret != nil && (provider == Resend || provider == Mailgun) {
		s := strings.TrimSpace(*secret)
		if provider == Resend {
			if _, err := svixKey(s); err != nil {
				return WebhookConfig{}, invalid("A Resend signing secret starts with whsec_ followed by base64.")
			}
		}
		if s == "" {
			return WebhookConfig{}, invalid("The signing key cannot be empty.")
		}
		st.Secret = s
	}
	saved, err := w.store.SaveProviderWebhook(ctx, st)
	if err != nil {
		return WebhookConfig{}, err
	}
	return view(saved)
}

// Deliveries returns recent webhook calls, newest first.
func (w *Webhooks) Deliveries() []Delivery {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Delivery{}, w.deliveries...)
}

// SendTest sends a "delivered" event for a made-up message and returns
// how the app answered.
func (w *Webhooks) SendTest(ctx context.Context, provider string) (Delivery, error) {
	st, err := w.settings(ctx, provider)
	if err != nil {
		return Delivery{}, err
	}
	if st.URL == "" {
		return Delivery{}, invalid("Enter the URL to send events to.")
	}
	now := time.Now().UTC()
	msg := sentMessage{
		provider: provider,
		id:       "envry-test-" + randomHex(6),
		from:     "Envry <test@envry.local>",
		sender:   "test@envry.local",
		subject:  "Envry webhook test",
		to:       []string{"delivered@test.local"},
		at:       now,
		meta:     eventMeta{domain: "envry.local"},
	}
	ds := w.send(ctx, st, msg, EventDelivered, msg.to)
	return ds[0], nil
}

// sentMessage is what events say about a captured message.
type sentMessage struct {
	provider string
	id       string // Envry's message id, which the API returned
	from     string // From header
	sender   string // envelope sender
	subject  string
	to       []string // envelope recipients
	at       time.Time
	size     int64
	meta     eventMeta
}

// eventMeta is what the app attached when sending, echoed in events.
type eventMeta struct {
	tags   []string          // categories (SendGrid), Tag (Postmark), o:tag (Mailgun), tag names
	values map[string]string // custom_args (SendGrid), Metadata (Postmark), v: variables (Mailgun), tags (Resend, SES)
	domain string            // Mailgun sending domain
	stream string            // Postmark message stream
}

// afterSend schedules the events for a captured message: "delivered" or
// "bounced" for each recipient, then "complained" for complaint@...
func (w *Webhooks) afterSend(msg sentMessage) {
	ctx := context.Background()
	st, err := w.settings(ctx, msg.provider)
	if err != nil || !st.Enabled || st.URL == "" {
		return
	}
	var delivered, bounced, complained []string
	for _, rcpt := range msg.to {
		switch outcome(rcpt) {
		case EventBounced:
			bounced = append(bounced, rcpt)
		case EventComplained:
			delivered = append(delivered, rcpt)
			complained = append(complained, rcpt)
		default:
			delivered = append(delivered, rcpt)
		}
	}
	go func() {
		time.Sleep(w.delay)
		if len(delivered) > 0 {
			w.send(ctx, st, msg, EventDelivered, delivered)
		}
		if len(bounced) > 0 {
			w.send(ctx, st, msg, EventBounced, bounced)
		}
		if len(complained) > 0 {
			time.Sleep(w.delay)
			w.send(ctx, st, msg, EventComplained, complained)
		}
	}()
}

// outcome is what happens to mail for an address: bounce@... and
// bounce+anything@... bounce, complaint@... is delivered and then
// reported as spam, and everything else is delivered.
func outcome(address string) string {
	local := strings.ToLower(address)
	if i := strings.LastIndexByte(local, '@'); i >= 0 {
		local = local[:i]
	}
	switch {
	case local == "bounce" || strings.HasPrefix(local, "bounce+"):
		return EventBounced
	case local == "complaint" || strings.HasPrefix(local, "complaint+"):
		return EventComplained
	}
	return EventDelivered
}

// request is one HTTP call to make.
type request struct {
	recipient   string // empty when one call covers every recipient
	body        []byte
	contentType string
	header      http.Header
}

// send makes the calls for one event and records each of them.
func (w *Webhooks) send(ctx context.Context, st store.ProviderWebhook, msg sentMessage, event string, rcpts []string) []Delivery {
	reqs, err := buildRequests(st, msg, event, rcpts, time.Now().UTC())
	if err != nil {
		w.log.Error("build webhook event", "provider", msg.provider, "err", err)
		return []Delivery{w.record(Delivery{Provider: msg.provider, Event: event, MessageID: msg.id, URL: st.URL, Error: err.Error(), SentAt: time.Now().UTC()})}
	}
	var out []Delivery
	for _, rq := range reqs {
		out = append(out, w.record(w.post(ctx, st.URL, msg, event, rq)))
	}
	return out
}

func (w *Webhooks) post(ctx context.Context, target string, msg sentMessage, event string, rq request) Delivery {
	d := Delivery{
		Provider: msg.provider, Event: event, MessageID: msg.id, Recipient: rq.recipient,
		URL: redactURL(target), SentAt: time.Now().UTC(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(rq.body))
	if err != nil {
		d.Error = err.Error()
		return d
	}
	for k, v := range rq.header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", rq.contentType)
	start := time.Now()
	res, err := w.client.Do(req)
	d.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		d.Error = err.Error()
		return d
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxResponseText))
	d.Status, d.Response = res.StatusCode, string(body)
	if res.StatusCode >= 300 {
		d.Error = res.Status
	}
	return d
}

// record adds a call to the log and announces it.
func (w *Webhooks) record(d Delivery) Delivery {
	w.mu.Lock()
	w.nextID++
	d.ID = w.nextID
	w.deliveries = append([]Delivery{d}, w.deliveries...)
	if len(w.deliveries) > maxDeliveries {
		w.deliveries = w.deliveries[:maxDeliveries]
	}
	w.mu.Unlock()
	w.store.Bus().Publish(events.Event{Type: WebhookSent, Data: d})
	if d.Error != "" {
		w.log.Warn("webhook call failed", "provider", d.Provider, "event", d.Event, "url", d.URL, "err", d.Error)
	}
	return d
}

// redactURL hides a password in the URL (Postmark sends basic auth that way).
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	if _, has := u.User.Password(); has {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	return u.String()
}

func known(provider string) bool {
	for _, p := range Providers {
		if p == provider {
			return true
		}
	}
	return false
}

func needsSecret(provider string) bool {
	return provider == Resend || provider == SendGrid || provider == Mailgun
}

// newSecret makes a provider's signing secret: a Svix secret for Resend,
// an ECDSA P-256 key for SendGrid, a signing key for Mailgun.
func newSecret(provider string) (string, error) {
	switch provider {
	case Resend:
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return "whsec_" + base64.StdEncoding.EncodeToString(b), nil
	case SendGrid:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", err
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
	default:
		return randomHex(16), nil
	}
}

func view(st store.ProviderWebhook) (WebhookConfig, error) {
	c := WebhookConfig{Provider: st.Provider, URL: st.URL, Enabled: st.Enabled, Signing: "none"}
	switch st.Provider {
	case Resend:
		c.Secret, c.Signing = st.Secret, "svix"
	case Mailgun:
		c.Secret, c.Signing = st.Secret, "hmac"
	case SendGrid:
		key, err := sendgridKey(st.Secret)
		if err != nil {
			return WebhookConfig{}, err
		}
		der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return WebhookConfig{}, err
		}
		c.VerificationKey, c.Signing = base64.StdEncoding.EncodeToString(der), "ecdsa"
	}
	return c, nil
}

func sendgridKey(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("sendgrid webhook key is missing")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sendgrid webhook key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("sendgrid webhook key is not an ECDSA key")
	}
	return key, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
