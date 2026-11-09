package emulate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/mail"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// Mailgun: POST /v3/{domain}/messages and /v3/{domain}/messages.mime
// (https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/messages),
// as multipart or URL-encoded forms. The Node SDK takes the base URL
// through its url option.

type mgResult struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func (s *Server) mailgunRoutes(r chi.Router) {
	r.Post("/v3/{domain}/messages", s.mailgunMessages)
	r.Post("/v3/{domain}/messages.mime", s.mailgunMIME)
}

func (s *Server) mailgunMessages(w http.ResponseWriter, r *http.Request) {
	form, err := mailgunForm(r)
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	msgs, meta, err := mailgunMessages(form)
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	meta.domain = chi.URLParam(r, "domain")
	if err := s.simulate(r.Context(), recipients(msgs...)); err != nil {
		s.mailgunError(w, err)
		return
	}
	var first string
	for _, m := range msgs {
		id, err := s.deliver(r.Context(), r, Mailgun, m, meta)
		if err != nil {
			s.mailgunError(w, err)
			return
		}
		if first == "" {
			first = id
		}
	}
	writeJSON(w, http.StatusOK, mgResult{ID: mailgunID(first, chi.URLParam(r, "domain")), Message: "Queued. Thank you."})
}

func (s *Server) mailgunMIME(w http.ResponseWriter, r *http.Request) {
	form, err := mailgunForm(r)
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	to, err := addresses("to", form.values["to"])
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	if len(to) == 0 {
		s.mailgunError(w, invalid("to parameter is missing"))
		return
	}
	files := form.files["message"]
	if len(files) == 0 {
		s.mailgunError(w, invalid("message parameter is missing"))
		return
	}
	raw, err := readFile(files[0])
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	var from string
	if p, err := mimeparse.Parse(raw); err == nil {
		from = p.From.Address
	}
	rcpts := make([]string, len(to))
	for i, a := range to {
		rcpts[i] = a.Address
	}
	if err := s.simulate(r.Context(), rcpts); err != nil {
		s.mailgunError(w, err)
		return
	}
	id, err := s.capture(r.Context(), r, Mailgun, from, rcpts, raw, eventMeta{domain: chi.URLParam(r, "domain")})
	if err != nil {
		s.mailgunError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mgResult{ID: mailgunID(id, chi.URLParam(r, "domain")), Message: "Queued. Thank you."})
}

// mailgunID is a Mailgun-style message id: <id@domain>.
func mailgunID(id, domain string) string { return "<" + id + "@" + domain + ">" }

type form struct {
	values map[string][]string
	files  map[string][]*multipart.FileHeader
}

func mailgunForm(r *http.Request) (form, error) {
	err := r.ParseMultipartForm(32 << 20)
	if err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return form{}, invalid("The request is larger than Envry accepts (%d bytes).", tooBig.Limit)
		}
		return form{}, invalid("Could not read the form: %v", err)
	}
	f := form{values: r.PostForm}
	if r.MultipartForm != nil {
		f.values = r.MultipartForm.Value
		f.files = r.MultipartForm.File
	}
	return f, nil
}

func (f form) get(key string) string {
	if v := f.values[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// mailgunMessages returns the messages to capture (several with batch
// sending) and the tags and variables their events echo.
func mailgunMessages(f form) ([]compose.Message, eventMeta, error) {
	if f.get("from") == "" {
		return nil, eventMeta{}, invalid("from parameter is missing")
	}
	from, err := address("from", f.get("from"))
	if err != nil {
		return nil, eventMeta{}, invalid("'from' parameter is not a valid address. please check documentation")
	}
	m := compose.Message{From: from, Subject: f.get("subject"), Text: f.get("text"), HTML: f.get("html")}
	for _, field := range []struct {
		name string
		out  *[]*mail.Address
	}{{"to", &m.To}, {"cc", &m.Cc}, {"bcc", &m.Bcc}} {
		if *field.out, err = addresses(field.name, f.values[field.name]); err != nil {
			return nil, eventMeta{}, invalid("'%s' parameter is not a valid address. please check documentation", field.name)
		}
	}
	if len(m.To) == 0 {
		return nil, eventMeta{}, invalid("to parameter is missing")
	}
	if tpl := f.get("template"); tpl != "" && m.Text == "" && m.HTML == "" {
		var vars any
		_ = json.Unmarshal([]byte(f.get("t:variables")), &vars)
		m.Text = templateNote("Mailgun", tpl, vars)
	} else if m.Text == "" && m.HTML == "" {
		return nil, eventMeta{}, invalid("Need at least one of 'text', 'html' or 'template' parameters specified")
	}

	// h:X-Header adds a header; o:tag and v:var become the headers Mailgun
	// adds to the mail it delivers.
	keys := make([]string, 0, len(f.values))
	for k := range f.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vars := map[string]string{}
	for _, k := range keys {
		for _, v := range f.values[k] {
			switch {
			case strings.EqualFold(k, "h:Reply-To"):
				if m.ReplyTo, err = addresses("h:Reply-To", []string{v}); err != nil {
					return nil, eventMeta{}, err
				}
			case strings.HasPrefix(k, "h:"):
				m.Headers = append(m.Headers, compose.Header{Name: k[2:], Value: v})
			case k == "o:tag":
				m.Headers = append(m.Headers, compose.Header{Name: "X-Mailgun-Tag", Value: v})
			case strings.HasPrefix(k, "v:"):
				vars[k[2:]] = v
			}
		}
	}
	meta := eventMeta{tags: f.values["o:tag"], values: vars}
	if len(vars) > 0 {
		b, _ := json.Marshal(vars)
		m.Headers = append(m.Headers, compose.Header{Name: "X-Mailgun-Variables", Value: string(b)})
	}

	for _, field := range []string{"attachment", "inline"} {
		for _, fh := range f.files[field] {
			data, err := readFile(fh)
			if err != nil {
				return nil, eventMeta{}, err
			}
			a := compose.Attachment{Filename: fh.Filename, ContentType: fh.Header.Get("Content-Type"), Data: data}
			if field == "inline" {
				a.ContentID = fh.Filename // Mailgun's HTML refers to inline files as cid:<file name>
			}
			m.Attachments = append(m.Attachments, a)
		}
	}

	// Batch sending: with recipient-variables, each To recipient gets its
	// own message with %recipient.key% filled in.
	rv := f.get("recipient-variables")
	if rv == "" {
		return []compose.Message{m}, meta, nil
	}
	var perRecipient map[string]map[string]any
	if err := json.Unmarshal([]byte(rv), &perRecipient); err != nil {
		return nil, eventMeta{}, invalid("recipient-variables parameter is not a valid JSON")
	}
	var out []compose.Message
	for _, to := range m.To {
		one := m
		one.To = []*mail.Address{to}
		for key, value := range perRecipient[to.Address] {
			placeholder := "%recipient." + key + "%"
			one.Subject = strings.ReplaceAll(one.Subject, placeholder, fmt.Sprint(value))
			one.Text = strings.ReplaceAll(one.Text, placeholder, fmt.Sprint(value))
			one.HTML = strings.ReplaceAll(one.HTML, placeholder, fmt.Sprint(value))
		}
		out = append(out, one)
	}
	return out, meta, nil
}

func readFile(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// mailgunError answers like Mailgun: 400 {"message": ...}.
func (s *Server) mailgunError(w http.ResponseWriter, err error) {
	var rejected errRejected
	switch {
	case errors.As(err, &rejected):
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	case errors.As(err, new(errThrottled)):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": err.Error()})
		return
	}
	if isInvalid(err) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	s.log.Error("mailgun emulation", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
}
