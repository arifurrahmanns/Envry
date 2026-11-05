// Package compose builds RFC 5322 messages for the API's Compose and the
// `envry send` command.
package compose

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// Attachment is a file attached to a composed message.
type Attachment struct {
	Filename    string
	ContentType string // detected from the file name when empty
	Data        []byte
}

// Message describes what to build. Bcc recipients are used for delivery but
// never written to the headers.
type Message struct {
	From        *mail.Address
	To, Cc, Bcc []*mail.Address
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
	Date        time.Time // zero means now
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
// given, so HTML-only mail can be tested too), and attachments wrap
// everything in multipart/mixed.
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
	h.SetSubject(m.Subject)
	if err := h.GenerateMessageIDWithHostname("envry.local"); err != nil {
		return nil, fmt.Errorf("compose: Message-ID: %w", err)
	}

	var buf bytes.Buffer
	if m.HTML == "" && len(m.Attachments) == 0 {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if err := writeAndClose(w, []byte(m.Text)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	iw, err := mw.CreateInline()
	if err != nil {
		return nil, err
	}
	parts := []struct{ mediaType, body string }{}
	if m.Text != "" || m.HTML == "" {
		parts = append(parts, struct{ mediaType, body string }{"text/plain", m.Text})
	}
	if m.HTML != "" {
		parts = append(parts, struct{ mediaType, body string }{"text/html", m.HTML})
	}
	for _, p := range parts {
		var ph gomail.InlineHeader
		ph.SetContentType(p.mediaType, map[string]string{"charset": "utf-8"})
		ph.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := iw.CreatePart(ph)
		if err != nil {
			return nil, err
		}
		if err := writeAndClose(pw, []byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := iw.Close(); err != nil {
		return nil, err
	}

	for _, a := range m.Attachments {
		var ah gomail.AttachmentHeader
		ct := a.ContentType
		if ct == "" {
			ct = ContentTypeFor(a.Filename)
		}
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil {
			mediaType, params = "application/octet-stream", nil
		}
		ah.SetContentType(mediaType, params)
		ah.SetFilename(a.Filename)
		ah.Set("Content-Transfer-Encoding", "base64")
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if err := writeAndClose(aw, a.Data); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

func writeAndClose(w io.WriteCloser, data []byte) error {
	if _, err := w.Write(data); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
