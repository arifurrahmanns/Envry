// Package compose builds RFC 5322 messages for the API's Compose and the
// `envry send` command.
package compose

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
)

// Attachment is a file attached to a composed message.
type Attachment struct {
	Filename    string
	ContentType string // detected from the file name when empty
	Data        []byte
	// ContentID, when set, makes it an inline part the HTML can show as
	// <img src="cid:...">.
	ContentID string
}

// Header is an extra header field, such as X-Campaign or List-Unsubscribe.
type Header struct {
	Name, Value string
}

// Message describes what to build. Bcc recipients are used for delivery but
// never written to the headers.
type Message struct {
	From        *mail.Address
	To, Cc, Bcc []*mail.Address
	ReplyTo     []*mail.Address
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
	// Headers are added as given, except the ones Build writes itself
	// (From, To, Subject, Content-Type...), which are ignored.
	Headers []Header
	Date    time.Time // zero means now
}

// Recipients returns every envelope recipient: To, Cc and Bcc.
func (m Message) Recipients() []string {
	var out []string
	for _, list := range [][]*mail.Address{m.To, m.Cc, m.Bcc} {
		for _, a := range list {
			out = append(out, a.Address)
		}
	}
	return out
}

// ParseAddresses parses each entry as an address ("Name <a@b>" or "a@b").
func ParseAddresses(list []string) ([]*mail.Address, error) {
	out := make([]*mail.Address, 0, len(list))
	for _, raw := range list {
		a, err := mail.ParseAddress(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q", raw)
		}
		out = append(out, a)
	}
	return out, nil
}

// Build renders the message. A text body alone becomes text/plain, HTML
// becomes multipart/alternative (with the text part only when one is
// given, so HTML-only mail can be tested too), inline images join it in
// multipart/related, and other attachments wrap everything in
// multipart/mixed.
func Build(m Message) ([]byte, error) {
	if m.From == nil {
		return nil, errors.New("compose: From is required")
	}
	if len(m.Recipients()) == 0 {
		return nil, errors.New("compose: at least one recipient is required")
	}
	if m.Date.IsZero() {
		m.Date = time.Now()
	}

	var h gomail.Header
	h.SetDate(m.Date)
	h.SetAddressList("From", []*gomail.Address{m.From})
	if len(m.To) > 0 {
		h.SetAddressList("To", m.To)
	}
	if len(m.Cc) > 0 {
		h.SetAddressList("Cc", m.Cc)
	}
	if len(m.ReplyTo) > 0 {
		h.SetAddressList("Reply-To", m.ReplyTo)
	}
	h.SetSubject(m.Subject)
	if err := h.GenerateMessageIDWithHostname("envry.local"); err != nil {
		return nil, fmt.Errorf("compose: Message-ID: %w", err)
	}
	for _, x := range m.Headers {
		// Written as given, so X-Entity-Ref-ID keeps its case. Values with
		// line breaks or non-ASCII text are encoded, so they cannot start
		// a header of their own.
		name := strings.TrimSpace(x.Name)
		if headerName.MatchString(name) && !reserved[strings.ToLower(name)] {
			h.AddRaw([]byte(name + ": " + mime.QEncoding.Encode("utf-8", x.Value) + "\r\n"))
		}
	}

	h.Set("MIME-Version", "1.0")

	var inline, files []Attachment
	for _, a := range m.Attachments {
		if a.ContentID != "" {
			inline = append(inline, a)
		} else {
			files = append(files, a)
		}
	}
	body := textPart("text/plain", m.Text)
	if m.HTML != "" {
		var alt []part
		if m.Text != "" {
			alt = append(alt, textPart("text/plain", m.Text))
		}
		body = multipart("alternative", append(alt, textPart("text/html", m.HTML))...)
	}
	// Inline images travel with the HTML that shows them.
	if len(inline) > 0 {
		related := []part{body}
		for _, a := range inline {
			related = append(related, attachmentPart(a))
		}
		body = multipart("related", related...)
	}
	root := body
	if len(files) > 0 {
		mixed := []part{body}
		for _, a := range files {
			mixed = append(mixed, attachmentPart(a))
		}
		root = multipart("mixed", mixed...)
	}

	// The outermost part's fields are the message's own.
	for f := root.header.Fields(); f.Next(); {
		h.Set(f.Key(), f.Value())
	}
	var buf bytes.Buffer
	w, err := message.CreateWriter(&buf, h.Header)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	if err := root.write(w); err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	return buf.Bytes(), nil
}

// part is one node of the MIME tree: a leaf with a body, or a multipart
// with parts.
type part struct {
	header message.Header
	body   []byte
	parts  []part
}

func textPart(mediaType, body string) part {
	var h message.Header
	h.SetContentType(mediaType, map[string]string{"charset": "utf-8"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	return part{header: h, body: []byte(body)}
}

func multipart(subtype string, parts ...part) part {
	var h message.Header
	h.SetContentType("multipart/"+subtype, nil)
	return part{header: h, parts: parts}
}

// attachmentPart is a file, or an inline image when it has a Content-ID.
func attachmentPart(a Attachment) part {
	ct := a.ContentType
	if ct == "" {
		ct = ContentTypeFor(a.Filename)
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType, params = "application/octet-stream", nil
	}
	var h message.Header
	h.SetContentType(mediaType, params)
	disposition := "attachment"
	if a.ContentID != "" {
		disposition = "inline"
		h.Set("Content-Id", "<"+strings.Trim(a.ContentID, "<>")+">")
	}
	var dispParams map[string]string
	if a.Filename != "" {
		dispParams = map[string]string{"filename": a.Filename}
	}
	h.SetContentDisposition(disposition, dispParams)
	h.Set("Content-Transfer-Encoding", "base64")
	return part{header: h, body: a.Data}
}

// write writes the part's body, or its parts, to w and closes it.
func (p part) write(w *message.Writer) error {
	if p.parts == nil {
		if _, err := w.Write(p.body); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	for _, c := range p.parts {
		cw, err := w.CreatePart(c.header)
		if err != nil {
			return err
		}
		if err := c.write(cw); err != nil {
			return err
		}
	}
	return w.Close()
}

// headerName is a field name: printable ASCII without spaces or colons.
var headerName = regexp.MustCompile(`^[!-9;-~]+$`)

// reserved are the headers Build writes; extra headers cannot replace them.
var reserved = map[string]bool{
	"from": true, "to": true, "cc": true, "bcc": true, "reply-to": true, "subject": true, "date": true,
	"message-id": true, "mime-version": true, "content-type": true, "content-transfer-encoding": true,
}

// common maps attachment extensions to media types the same way on every
// OS (mime.TypeByExtension depends on the system's registry or mime.types).
var common = map[string]string{
	".csv":  "text/csv; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".ics":  "text/calendar; charset=utf-8",
	".json": "application/json",
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

// ContentTypeFor guesses a file's media type from its name.
func ContentTypeFor(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ct, ok := common[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
