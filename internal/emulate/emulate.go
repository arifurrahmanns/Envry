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
	"net"
	"net/http"
	"net/mail"
	"sort"
	"strings"

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
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxSize)
			next.ServeHTTP(w, r)
		})
	})
	r.Route("/resend", s.resendRoutes)
	r.Route("/sendgrid", s.sendgridRoutes)
	r.Route("/postmark", s.postmarkRoutes)
	r.Route("/mailgun", s.mailgunRoutes)
	r.Route("/ses", s.sesRoutes)
	return r
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
