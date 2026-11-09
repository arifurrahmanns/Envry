// Package emulate answers the send APIs of email providers (Resend,
// SendGrid, Postmark, Mailgun and Amazon SES), so an app that sends mail
// through a provider's SDK can point the SDK at Envry in development and
// see every message without sending any. Each provider's request is turned
// into a message and captured like mail that arrived over SMTP; the reply
// has the shape that provider's SDK expects. Any API key is accepted.
package emulate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/mail"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
	"github.com/arifurrahmanns/envry/internal/store"
)

// Provider names, also used in a message's source ("api:resend").
const (
	Resend   = "resend"
	SendGrid = "sendgrid"
	Postmark = "postmark"
	Mailgun  = "mailgun"
	SES      = "ses"
)

// Source is the message source recorded for mail sent through provider.
func Source(provider string) string { return store.SourceAPI + ":" + provider }

// Server holds what the provider handlers share.
type Server struct {
	store    *store.Store
	log      *slog.Logger
	maxSize  int64
	webhooks *Webhooks
	failures Failures
}

// New returns the emulator. maxSize caps a request body, as SMTP caps a
// message (attachments arrive base64-encoded, so a third more is allowed).
func New(st *store.Store, log *slog.Logger, maxSize int64) *Server {
	if maxSize <= 0 {
		maxSize = 25 << 20
	}
	return &Server{store: st, log: log, maxSize: maxSize + maxSize/3 + 1<<20, webhooks: newWebhooks(st, log)}
}

// Routes returns the router to mount at /emulate: each provider's API
// under its own prefix, at the provider's own paths.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.limitBody)
	r.Route("/resend", s.resendRoutes)
	r.Route("/sendgrid", s.sendgridRoutes)
	r.Route("/postmark", s.postmarkRoutes)
	r.Route("/mailgun", s.mailgunRoutes)
	r.Route("/ses", s.sesRoutes)
	return r
}

// RootRoutes answers the same APIs at the providers' own paths from the
// server root (POST /email, /v3/{domain}/messages...), for SDKs that keep
// only the host of a base URL, such as postmarker and mailgun-php. They
// are POST routes on paths the web UI does not use. SES's query API stays
// under /emulate/ses: it posts to "/", the web UI's own page.
func (s *Server) RootRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.limitBody)
		s.resendRoutes(r)
		s.sendgridRoutes(r)
		s.postmarkRoutes(r)
		s.mailgunRoutes(r)
		r.Post("/v2/email/outbound-emails", s.sesV2Send)
	})
}

func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxSize)
		next.ServeHTTP(w, r)
	})
}

// deliver builds a message and captures it as sent through provider.
func (s *Server) deliver(ctx context.Context, r *http.Request, provider string, m compose.Message, meta eventMeta) (string, error) {
	raw, err := compose.Build(m)
	if err != nil {
		return "", err
	}
	return s.capture(ctx, r, provider, m.From.Address, m.Recipients(), raw, meta)
}

// capture stores a raw message with its envelope and schedules its
// delivery events; meta is what the app attached for them.
func (s *Server) capture(ctx context.Context, r *http.Request, provider, from string, rcpts []string, raw []byte, meta eventMeta) (string, error) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	sum, err := s.store.Ingest(ctx, store.IngestInput{
		EnvelopeFrom: from,
		Recipients:   rcpts,
		Raw:          raw,
		Source:       Source(provider),
		ClientIP:     ip,
	})
	if err != nil {
		return "", err
	}
	s.log.Debug("captured provider API message", "provider", provider, "id", sum.ID)
	header := sum.From.Address
	if sum.From.Name != "" {
		header = (&mail.Address{Name: sum.From.Name, Address: sum.From.Address}).String()
	}
	s.webhooks.afterSend(sentMessage{
		provider: provider, id: sum.ID, from: header, sender: from, subject: sum.Subject,
		to: rcpts, at: sum.ReceivedAt, size: sum.Size, meta: meta,
	})
	return sum.ID, nil
}

// errInvalid marks a request the provider would reject; its text is shown
// to the caller in the provider's error format.
type errInvalid struct{ msg string }

func (e errInvalid) Error() string { return e.msg }

func invalid(format string, args ...any) error { return errInvalid{fmt.Sprintf(format, args...)} }

// IsInvalid reports an error in what was asked (an invalid request or
// setting), as opposed to Envry failing.
func IsInvalid(err error) bool { return isInvalid(err) }

// isInvalid reports a request error (as opposed to Envry failing).
func isInvalid(err error) bool {
	var e errInvalid
	return errors.As(err, &e)
}

// address parses "Name <a@b>" or "a@b".
func address(field, s string) (*mail.Address, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(s))
	if err != nil {
		return nil, invalid("Invalid `%s` field: %q is not a valid email address.", field, s)
	}
	return a, nil
}

// addresses parses each entry, and each entry may itself be a
// comma-separated list ("a@b, Name <c@d>").
func addresses(field string, list []string) ([]*mail.Address, error) {
	var out []*mail.Address
	for _, s := range list {
		if strings.TrimSpace(s) == "" {
			continue
		}
		parsed, err := mail.ParseAddressList(s)
		if err != nil {
			return nil, invalid("Invalid `%s` field: %q is not a valid email address.", field, s)
		}
		out = append(out, parsed...)
	}
	return out, nil
}

// headerMap is a headers field: an object of names to values, or a list of
// {"name","value"} objects; an empty array (as PHP encodes an empty map)
// means no headers.
type headerMap map[string]string

func (h *headerMap) UnmarshalJSON(b []byte) error {
	var obj map[string]string
	if err := json.Unmarshal(b, &obj); err == nil {
		*h = obj
		return nil
	}
	var list []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return errors.New("headers must be an object of header names to values")
	}
	*h = headerMap{}
	for _, x := range list {
		(*h)[x.Name] = x.Value
	}
	return nil
}

// stringList is a JSON field that may be one string or an array of them.
type stringList []string

func (l *stringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*l = stringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return errors.New("must be a string or an array of strings")
	}
	*l = many
	return nil
}

// fileContent is attachment content: base64 text, or (as older SDKs send
// a Node.js Buffer) an array of byte values or {"type":"Buffer","data":[...]}.
type fileContent []byte

func (c *fileContent) UnmarshalJSON(b []byte) error {
	var text string
	if err := json.Unmarshal(b, &text); err == nil {
		data, err := decodeBase64(text)
		if err != nil {
			return err
		}
		*c = data
		return nil
	}
	var bytes []byte
	var numbers []int
	if err := json.Unmarshal(b, &numbers); err != nil {
		var buf struct {
			Data []int `json:"data"`
		}
		if err := json.Unmarshal(b, &buf); err != nil {
			return errors.New("attachment content must be base64 text or an array of bytes")
		}
		numbers = buf.Data
	}
	for _, n := range numbers {
		bytes = append(bytes, byte(n))
	}
	*c = bytes
	return nil
}

// decodeBase64 accepts standard or URL-safe base64, padded or not, with
// line breaks.
func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("attachment content is not valid base64")
}

// decodeJSON reads a JSON body. Unknown fields are ignored: providers add
// options (tracking, scheduling, tags) that do not change the message.
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return invalid("The request is larger than Envry accepts (%d bytes).", tooBig.Limit)
		}
		return invalid("Invalid JSON: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// headerList turns a header map into a list in a stable order.
func headerList(h map[string]string) []compose.Header {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]compose.Header, 0, len(names))
	for _, name := range names {
		out = append(out, compose.Header{Name: name, Value: h[name]})
	}
	return out
}

// templateNote is the text of a message sent with a provider template
// Envry cannot render: which template, and the data it was given.
func templateNote(provider, template string, data any) string {
	b, _ := json.MarshalIndent(data, "", "  ")
	note := fmt.Sprintf("Sent with %s template %s. Envry does not have the template, so this shows the data it was given.", provider, template)
	if len(b) > 0 && string(b) != "null" {
		note += "\n\nTemplate data:\n" + string(b)
	}
	return note
}

// Failures is Envry's failure simulation (--smtp-reject-rcpt,
// --smtp-reject-rate, --smtp-latency), applied to provider API requests
// as well, so an app's error handling can be tested whichever way it
// sends.
type Failures struct {
	// RejectRecipients are lower-cased address patterns ("*" wildcards):
	// a request to any of them gets the provider's "recipient refused" error.
	RejectRecipients []string
	// RejectRate is the percentage (0-100) of requests answered with the
	// provider's rate-limit error, a temporary failure apps should retry.
	RejectRate int
	// Latency delays every answer.
	Latency time.Duration
	// Roll returns a number in [0, 100) for RejectRate; tests replace it.
	Roll func() int
}

// SetFailures turns on failure simulation. Call before serving.
func (s *Server) SetFailures(f Failures) {
	if f.Roll == nil {
		f.Roll = func() int { return rand.IntN(100) }
	}
	s.failures = f
}

// errRejected is a recipient refused by failure simulation.
type errRejected struct{ address string }

func (e errRejected) Error() string {
	return fmt.Sprintf("%s was refused by Envry's failure simulation.", e.address)
}

// errThrottled is a request refused as rate limited by failure simulation.
type errThrottled struct{}

func (errThrottled) Error() string {
	return "Too many requests: Envry's failure simulation refused this request. Try again."
}

// simulate applies failure simulation to a request for these recipients,
// before anything is captured: the delay, a refused recipient, then the
// rate limit.
func (s *Server) simulate(ctx context.Context, rcpts []string) error {
	if err := s.delay(ctx); err != nil {
		return err
	}
	if err := s.refused(rcpts); err != nil {
		return err
	}
	return s.throttled()
}

// delay waits for the simulated latency.
func (s *Server) delay(ctx context.Context) error {
	if d := s.failures.Latency; d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// refused returns errRejected for the first recipient that failure
// simulation refuses.
func (s *Server) refused(rcpts []string) error {
	for _, rc := range rcpts {
		addr := store.NormalizeAddress(rc)
		for _, pattern := range s.failures.RejectRecipients {
			if ok, _ := path.Match(pattern, addr); ok {
				s.log.Debug("recipient refused by failure simulation", "rcpt", addr, "pattern", pattern)
				return errRejected{addr}
			}
		}
	}
	return nil
}

// throttled returns errThrottled for the simulated share of requests.
func (s *Server) throttled() error {
	if rate := s.failures.RejectRate; rate > 0 && s.failures.Roll() < rate {
		s.log.Debug("request refused by failure simulation", "rate", rate)
		return errThrottled{}
	}
	return nil
}

// recipients lists every message's envelope recipients.
func recipients(msgs ...compose.Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Recipients()...)
	}
	return out
}
