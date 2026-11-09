package emulate

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
	"github.com/arifurrahmanns/envry/internal/store"
)

type env struct {
	t      *testing.T
	store  *store.Store
	srv    *httptest.Server
	server *Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
	r := chi.NewRouter()
	r.Mount("/emulate", s.Routes())
	srv := httptest.NewServer(r)
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	return &env{t: t, store: st, srv: srv, server: s}
}

// post sends body (JSON unless it is already a reader) and returns the
// response and its body.
func (e *env) post(path, contentType string, body any) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case io.Reader:
		rd = b
	case string:
		rd = strings.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	res, err := http.Post(e.srv.URL+path, contentType, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res, out
}

// captured is a stored message with its parsed content.
type captured struct {
	*store.MessageDetail
	parsed  *mimeparse.Message
	headers map[string]string
}

func (e *env) message(id string) captured {
	e.t.Helper()
	ctx := context.Background()
	d, err := e.store.GetMessage(ctx, id)
	if err != nil {
		e.t.Fatalf("message %s: %v", id, err)
	}
	raw, err := e.store.Raw(ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		e.t.Fatal(err)
	}
	h := map[string]string{}
	for _, x := range p.Headers {
		h[strings.ToLower(x.Name)] = x.Value
	}
	return captured{d, p, h}
}

// header returns a header's value; names are case-insensitive.
func (c captured) header(name string) string { return c.headers[strings.ToLower(name)] }

func (e *env) count() int {
	e.t.Helper()
	page, err := e.store.ListMessages(context.Background(), store.ListOptions{})
	if err != nil {
		e.t.Fatal(err)
	}
	return page.Total
}

func (c captured) recipients() []string {
	var out []string
	for _, r := range c.Recipients {
		out = append(out, r.Address+":"+r.Type)
	}
	slices.Sort(out)
	return out
}

func (c captured) attachmentNames() []string {
	var out []string
	for _, a := range c.parsed.Attachments {
		name := a.Filename
		if a.ContentID != "" {
			name += "#" + a.ContentID
		}
		out = append(out, name)
	}
	return out
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestResend(t *testing.T) {
	e := newEnv(t)
	res, body := e.post("/emulate/resend/emails", "application/json", map[string]any{
		"from":     "Acme <onboarding@acme.test>",
		"to":       []string{"ada@test.local"},
		"cc":       "grace@test.local",
		"bcc":      []string{"audit@test.local"},
		"reply_to": "support@acme.test",
		"subject":  "Welcome",
		"html":     `<p>Hi</p><img src="cid:logo">`,
		"text":     "Hi",
		"headers":  map[string]string{"X-Entity-Ref-ID": "123"},
		"tags":     []map[string]string{{"name": "category", "value": "welcome"}},
		"attachments": []map[string]any{
			{"filename": "invoice.pdf", "content": b64("%PDF")},
			{"filename": "logo.png", "content": []int{137, 80, 78, 71}, "content_id": "logo"},
		},
	})
	var got struct{ ID string }
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil || got.ID == "" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	m := e.message(got.ID)
	if m.Source != "api:resend" || m.From.Address != "onboarding@acme.test" || m.From.Name != "Acme" || m.Subject != "Welcome" {
		t.Errorf("message = %+v", m.MessageSummary)
	}
	if want := []string{"ada@test.local:to", "audit@test.local:bcc", "grace@test.local:cc"}; !slices.Equal(m.recipients(), want) {
		t.Errorf("recipients = %v", m.recipients())
	}
	if !m.parsed.HasHTML || strings.TrimSpace(m.parsed.Text) != "Hi" || m.ReplyTo[0].Address != "support@acme.test" ||
		m.header("X-Entity-Ref-ID") != "123" || m.header("X-Resend-Tag") != "category=welcome" {
		t.Errorf("parsed = %+v headers = %v", m.parsed, m.headers)
	}
	if want := []string{"logo.png#logo", "invoice.pdf"}; !slices.Equal(m.attachmentNames(), want) {
		t.Errorf("attachments = %v", m.attachmentNames())
	}

	res, body = e.post("/emulate/resend/emails/batch", "application/json", []map[string]any{
		{"from": "a@acme.test", "to": "b@test.local", "subject": "1", "text": "one"},
		{"from": "a@acme.test", "to": "c@test.local", "subject": "2", "text": "two"},
	})
	var batch struct{ Data []struct{ ID string } }
	if res.StatusCode != 200 || json.Unmarshal(body, &batch) != nil || len(batch.Data) != 2 {
		t.Fatalf("batch: %d %s", res.StatusCode, body)
	}

	before := e.count()
	res, body = e.post("/emulate/resend/emails", "application/json", map[string]any{"to": "b@test.local", "subject": "x"})
	if res.StatusCode != 422 || !strings.Contains(string(body), `"name":"validation_error"`) || e.count() != before {
		t.Errorf("missing from: %d %s", res.StatusCode, body)
	}
}

func TestSendGrid(t *testing.T) {
	e := newEnv(t)
	res, body := e.post("/emulate/sendgrid/v3/mail/send", "application/json", map[string]any{
		"personalizations": []map[string]any{
			{"to": []map[string]string{{"email": "ada@test.local", "name": "Ada"}}, "cc": []map[string]string{{"email": "grace@test.local"}},
				"substitutions": map[string]string{"-name-": "Ada"}},
			{"to": []map[string]string{{"email": "alan@test.local"}}, "subject": "Just for Alan", "bcc": []map[string]string{{"email": "audit@test.local"}}},
		},
		"from":     map[string]string{"email": "hello@acme.test", "name": "Acme"},
		"reply_to": map[string]string{"email": "support@acme.test"},
		"subject":  "Hello -name-",
		"content": []map[string]string{
			{"type": "text/plain", "value": "Hi -name-"},
			{"type": "text/html", "value": `<p>Hi -name-</p><img src="cid:logo">`},
		},
		"attachments": []map[string]string{
			{"content": b64("png"), "filename": "logo.png", "type": "image/png", "disposition": "inline", "content_id": "logo"},
			{"content": b64("a,b"), "filename": "data.csv", "type": "text/csv"},
		},
		"headers":    map[string]string{"X-Campaign": "launch"},
		"categories": []string{"onboarding"},
	})
	if res.StatusCode != 202 || len(body) != 0 || res.Header.Get("X-Message-Id") == "" {
		t.Fatalf("%d %q headers %v", res.StatusCode, body, res.Header)
	}
	if e.count() != 2 {
		t.Fatalf("want one message per personalization, got %d", e.count())
	}
	first := e.message(res.Header.Get("X-Message-Id"))
	if first.Source != "api:sendgrid" || first.Subject != "Hello Ada" || strings.TrimSpace(first.parsed.Text) != "Hi Ada" ||
		first.From.Name != "Acme" || first.ReplyTo[0].Address != "support@acme.test" ||
		first.header("X-Campaign") != "launch" || first.header("X-SendGrid-Category") != "onboarding" {
		t.Errorf("first = %+v %v", first.MessageSummary, first.headers)
	}
	if want := []string{"ada@test.local:to", "grace@test.local:cc"}; !slices.Equal(first.recipients(), want) {
		t.Errorf("recipients = %v", first.recipients())
	}
	if want := []string{"logo.png#logo", "data.csv"}; !slices.Equal(first.attachmentNames(), want) {
		t.Errorf("attachments = %v", first.attachmentNames())
	}

	res, body = e.post("/emulate/sendgrid/v3/mail/send", "application/json", map[string]any{
		"personalizations": []map[string]any{{"to": []map[string]string{{"email": "ada@test.local"}}}},
		"from":             map[string]string{"email": "hello@acme.test"},
		"subject":          "No body",
	})
	if res.StatusCode != 400 || !strings.Contains(string(body), `"field":"content"`) {
		t.Errorf("missing content: %d %s", res.StatusCode, body)
	}
}

func TestPostmark(t *testing.T) {
	e := newEnv(t)
	res, body := e.post("/emulate/postmark/email", "application/json", map[string]any{
		"From":        "Acme <sender@acme.test>",
		"To":          `"Ada" <ada@test.local>, grace@test.local`,
		"Bcc":         "audit@test.local",
		"ReplyTo":     "support@acme.test",
		"Subject":     "Your receipt",
		"Tag":         "receipts",
		"HtmlBody":    `<p>Thanks</p><img src="cid:logo">`,
		"TextBody":    "Thanks",
		"Headers":     []map[string]string{{"Name": "X-Order", "Value": "42"}},
		"Metadata":    map[string]string{"customer": "7"},
		"Attachments": []map[string]string{{"Name": "logo.png", "Content": b64("png"), "ContentType": "image/png", "ContentID": "cid:logo"}},
	})
	var got pmResult
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil || got.MessageID == "" || got.ErrorCode != 0 || got.Message != "OK" ||
		got.SubmittedAt == "" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	m := e.message(got.MessageID)
	if m.Source != "api:postmark" || m.header("X-PM-Tag") != "receipts" || m.header("X-PM-Metadata-customer") != "7" ||
		m.header("X-Order") != "42" || m.ReplyTo[0].Address != "support@acme.test" {
		t.Errorf("message = %+v %v", m.MessageSummary, m.headers)
	}
	if want := []string{"ada@test.local:to", "audit@test.local:bcc", "grace@test.local:to"}; !slices.Equal(m.recipients(), want) {
		t.Errorf("recipients = %v", m.recipients())
	}
	if want := []string{"logo.png#logo"}; !slices.Equal(m.attachmentNames(), want) {
		t.Errorf("attachments = %v", m.attachmentNames())
	}

	// In a batch each message stands alone.
	res, body = e.post("/emulate/postmark/email/batch", "application/json", []map[string]any{
		{"From": "sender@acme.test", "To": "ada@test.local", "Subject": "1", "TextBody": "one"},
		{"From": "not an address", "To": "ada@test.local", "Subject": "2", "TextBody": "two"},
	})
	var batch []pmResult
	if res.StatusCode != 200 || json.Unmarshal(body, &batch) != nil || len(batch) != 2 ||
		batch[0].ErrorCode != 0 || batch[1].ErrorCode != pmInvalidEmail {
		t.Errorf("batch: %d %s", res.StatusCode, body)
	}

	res, body = e.post("/emulate/postmark/email/withTemplate", "application/json", map[string]any{
		"From": "sender@acme.test", "To": "ada@test.local", "TemplateAlias": "welcome",
		"TemplateModel": map[string]any{"name": "Ada"},
	})
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil {
		t.Fatalf("withTemplate: %d %s", res.StatusCode, body)
	}
	if m := e.message(got.MessageID); !strings.Contains(m.parsed.Text, "template welcome") || !strings.Contains(m.parsed.Text, `"name": "Ada"`) {
		t.Errorf("template text = %q", m.parsed.Text)
	}

	res, body = e.post("/emulate/postmark/email", "application/json", map[string]any{"From": "sender@acme.test", "Subject": "x", "TextBody": "x"})
	if res.StatusCode != 422 || !strings.Contains(string(body), `"ErrorCode":300`) {
		t.Errorf("no recipients: %d %s", res.StatusCode, body)
	}
}

func TestMailgun(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range [][2]string{
		{"from", "Acme <postmaster@mg.acme.test>"}, {"to", "ada@test.local"}, {"to", "Grace <grace@test.local>"},
		{"cc", "alan@test.local, ken@test.local"}, {"subject", "Hello"}, {"text", "Hi"}, {"html", `<img src="cid:logo.png">`},
		{"h:Reply-To", "support@acme.test"}, {"h:X-Campaign", "launch"}, {"o:tag", "welcome"}, {"v:user-id", "7"},
	} {
		_ = mw.WriteField(f[0], f[1])
	}
	for _, f := range [][3]string{{"attachment", "report.pdf", "%PDF"}, {"inline", "logo.png", "png"}} {
		w, _ := mw.CreateFormFile(f[0], f[1])
		_, _ = w.Write([]byte(f[2]))
	}
	mw.Close()
	res, body := e.post("/emulate/mailgun/v3/mg.acme.test/messages", mw.FormDataContentType(), &buf)
	var got mgResult
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil || got.Message != "Queued. Thank you." ||
		!strings.HasSuffix(got.ID, "@mg.acme.test>") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	m := e.message(strings.TrimSuffix(strings.TrimPrefix(got.ID, "<"), "@mg.acme.test>"))
	if m.Source != "api:mailgun" || m.ReplyTo[0].Address != "support@acme.test" || m.header("X-Campaign") != "launch" ||
		m.header("X-Mailgun-Tag") != "welcome" || m.header("X-Mailgun-Variables") != `{"user-id":"7"}` {
		t.Errorf("message = %+v %v", m.MessageSummary, m.headers)
	}
	if want := []string{"ada@test.local:to", "alan@test.local:cc", "grace@test.local:to", "ken@test.local:cc"}; !slices.Equal(m.recipients(), want) {
		t.Errorf("recipients = %v", m.recipients())
	}
	if want := []string{"logo.png#logo.png", "report.pdf"}; !slices.Equal(m.attachmentNames(), want) {
		t.Errorf("attachments = %v", m.attachmentNames())
	}

	// URL-encoded batch sending: one message per recipient.
	form := url.Values{
		"from": {"postmaster@mg.acme.test"}, "to": {"ada@test.local, grace@test.local"}, "subject": {"Hi %recipient.name%"},
		"text":                {"Hello %recipient.name%"},
		"recipient-variables": {`{"ada@test.local":{"name":"Ada"},"grace@test.local":{"name":"Grace"}}`},
	}
	before := e.count()
	res, body = e.post("/emulate/mailgun/v3/mg.acme.test/messages", "application/x-www-form-urlencoded", form.Encode())
	if res.StatusCode != 200 || e.count() != before+2 {
		t.Fatalf("batch: %d %s, %d new", res.StatusCode, body, e.count()-before)
	}

	// Raw MIME.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	_ = mw.WriteField("to", "ada@test.local")
	w, _ := mw.CreateFormFile("message", "message.mime")
	_, _ = w.Write([]byte("From: raw@acme.test\r\nTo: ada@test.local\r\nSubject: Raw\r\n\r\nRaw body\r\n"))
	mw.Close()
	res, body = e.post("/emulate/mailgun/v3/mg.acme.test/messages.mime", mw.FormDataContentType(), &buf)
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil {
		t.Fatalf("mime: %d %s", res.StatusCode, body)
	}
	if m := e.message(strings.TrimSuffix(strings.TrimPrefix(got.ID, "<"), "@mg.acme.test>")); m.Subject != "Raw" || m.EnvelopeFrom != "raw@acme.test" {
		t.Errorf("mime message = %+v", m.MessageSummary)
	}

	res, body = e.post("/emulate/mailgun/v3/mg.acme.test/messages", "application/x-www-form-urlencoded", "to=ada@test.local&text=x")
	if res.StatusCode != 400 || !strings.Contains(string(body), "from parameter is missing") {
		t.Errorf("missing from: %d %s", res.StatusCode, body)
	}
}

func TestSESv2(t *testing.T) {
	e := newEnv(t)
	res, body := e.post("/emulate/ses/v2/email/outbound-emails", "application/json", map[string]any{
		"FromEmailAddress": "Acme <no-reply@acme.test>",
		"Destination":      map[string]any{"ToAddresses": []string{"ada@test.local"}, "BccAddresses": []string{"audit@test.local"}},
		"ReplyToAddresses": []string{"support@acme.test"},
		"Content": map[string]any{"Simple": map[string]any{
			"Subject": map[string]string{"Data": "Verify your email"},
			"Body":    map[string]any{"Text": map[string]string{"Data": "Code 482913"}, "Html": map[string]string{"Data": "<p>Code 482913</p>"}},
			"Headers": []map[string]string{{"Name": "X-Flow", "Value": "signup"}},
			"Attachments": []map[string]string{
				{"RawContent": b64("png"), "FileName": "logo.png", "ContentType": "image/png", "ContentDisposition": "INLINE", "ContentId": "logo"},
			},
		}},
		"EmailTags": []map[string]string{{"Name": "flow", "Value": "signup"}},
	})
	var got struct{ MessageId string }
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil || got.MessageId == "" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	m := e.message(got.MessageId)
	if m.Source != "api:ses" || m.Subject != "Verify your email" || m.header("X-Flow") != "signup" ||
		m.header("X-SES-Tag") != "flow=signup" || m.ReplyTo[0].Address != "support@acme.test" {
		t.Errorf("message = %+v %v", m.MessageSummary, m.headers)
	}
	if want := []string{"ada@test.local:to", "audit@test.local:bcc"}; !slices.Equal(m.recipients(), want) {
		t.Errorf("recipients = %v", m.recipients())
	}

	// Raw content, as nodemailer's SES transport sends it; the headers name
	// the recipients when Destination is left out.
	raw := "From: Shop <shop@acme.test>\r\nTo: ada@test.local\r\nCc: grace@test.local\r\nBcc: audit@test.local\r\nSubject: Raw\r\n\r\nBody\r\n"
	res, body = e.post("/emulate/ses/v2/email/outbound-emails", "application/json", map[string]any{
		"Content": map[string]any{"Raw": map[string]string{"Data": b64(raw)}},
	})
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil {
		t.Fatalf("raw: %d %s", res.StatusCode, body)
	}
	m = e.message(got.MessageId)
	if m.EnvelopeFrom != "shop@acme.test" || m.Subject != "Raw" {
		t.Errorf("raw message = %+v", m.MessageSummary)
	}
	if want := []string{"ada@test.local:to", "audit@test.local:bcc", "grace@test.local:cc"}; !slices.Equal(m.recipients(), want) {
		t.Errorf("raw recipients = %v", m.recipients())
	}

	res, body = e.post("/emulate/ses/v2/email/outbound-emails", "application/json", map[string]any{
		"Destination": map[string]any{"ToAddresses": []string{"ada@test.local"}},
		"Content":     map[string]any{"Simple": map[string]any{"Subject": map[string]string{"Data": "x"}}},
	})
	if res.StatusCode != 400 || res.Header.Get("X-Amzn-Errortype") != "BadRequestException" {
		t.Errorf("missing from: %d %s", res.StatusCode, body)
	}
}

func TestSESv1(t *testing.T) {
	e := newEnv(t)
	form := url.Values{
		"Action": {"SendEmail"}, "Source": {"no-reply@acme.test"},
		"Destination.ToAddresses.member.1": {"ada@test.local"}, "Destination.ToAddresses.member.2": {"grace@test.local"},
		"Message.Subject.Data": {"Hello"}, "Message.Body.Text.Data": {"Hi"}, "Message.Body.Html.Data": {"<p>Hi</p>"},
	}
	res, body := e.post("/emulate/ses/", "application/x-www-form-urlencoded", form.Encode())
	var got struct {
		Result struct {
			MessageID string `xml:"MessageId"`
		} `xml:"SendEmailResult"`
	}
	if res.StatusCode != 200 || xmlUnmarshal(body, &got) != nil || got.Result.MessageID == "" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if m := e.message(got.Result.MessageID); m.Source != "api:ses" || m.Subject != "Hello" || len(m.Recipients) != 2 || !m.parsed.HasHTML {
		t.Errorf("message = %+v", m.MessageSummary)
	}

	// Laravel's ses driver: SendRawEmail.
	raw := "From: app@acme.test\r\nTo: ada@test.local\r\nSubject: Laravel\r\n\r\nBody\r\n"
	form = url.Values{"Action": {"SendRawEmail"}, "RawMessage.Data": {b64(raw)}, "Destinations.member.1": {"ada@test.local"}}
	res, body = e.post("/emulate/ses", "application/x-www-form-urlencoded", form.Encode())
	var rawGot struct {
		Result struct {
			MessageID string `xml:"MessageId"`
		} `xml:"SendRawEmailResult"`
	}
	if res.StatusCode != 200 || xmlUnmarshal(body, &rawGot) != nil || rawGot.Result.MessageID == "" {
		t.Fatalf("raw: %d %s", res.StatusCode, body)
	}
	if m := e.message(rawGot.Result.MessageID); m.Subject != "Laravel" || m.EnvelopeFrom != "app@acme.test" {
		t.Errorf("raw message = %+v", m.MessageSummary)
	}

	res, body = e.post("/emulate/ses/", "application/x-www-form-urlencoded", "Action=GetSendQuota")
	if res.StatusCode != 400 || !strings.Contains(string(body), "<Code>InvalidAction</Code>") {
		t.Errorf("unknown action: %d %s", res.StatusCode, body)
	}
}

func xmlUnmarshal(b []byte, v any) error { return xml.Unmarshal(b, v) }

// Resend's headers may arrive as an object, as a list of name and value
// pairs, or (from PHP, with no headers) as an empty array.
func TestResendHeaderShapes(t *testing.T) {
	e := newEnv(t)
	for _, headers := range []any{
		map[string]string{"X-Ref": "1"},
		[]map[string]string{{"name": "X-Ref", "value": "1"}},
		[]any{},
	} {
		res, body := e.post("/emulate/resend/emails", "application/json", map[string]any{
			"from": "a@acme.test", "to": "ada@test.local", "subject": "x", "text": "x", "headers": headers,
		})
		if res.StatusCode != 200 {
			t.Errorf("headers %v: %d %s", headers, res.StatusCode, body)
		}
	}
}
