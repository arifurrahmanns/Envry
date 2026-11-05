package api

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"time"

	gomail "github.com/emersion/go-message/mail"

	"github.com/arifurrahmanns/envry/internal/store"
)

const defaultComposeFrom = "envry@localhost"

type sendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

type sendResponse struct {
	ID string `json:"id"`
}

// handleSend composes a message and captures it like any other: it appears
// in Sent and in every recipient's mailbox. Nothing leaves the machine.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.From == "" {
		req.From = defaultComposeFrom
	}
	from, err := mail.ParseAddress(req.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid from address %q", req.From))
		return
	}
	var to, cc, bcc []*mail.Address
	for _, list := range []struct {
		in  []string
		out *[]*mail.Address
	}{{req.To, &to}, {req.Cc, &cc}, {req.Bcc, &bcc}} {
		for _, raw := range list.in {
			a, err := mail.ParseAddress(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid recipient address %q", raw))
				return
			}
			*list.out = append(*list.out, a)
		}
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		writeError(w, http.StatusBadRequest, "at least one recipient is required")
		return
	}

	raw, err := composeMessage(from, to, cc, req.Subject, req.Text, req.HTML, time.Now())
	if err != nil {
		s.internalError(w, err)
		return
	}
	var rcpts []string
	for _, a := range append(append(append([]*mail.Address{}, to...), cc...), bcc...) {
		rcpts = append(rcpts, a.Address)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	sum, err := s.store.Ingest(r.Context(), store.IngestInput{
		EnvelopeFrom: from.Address,
		Recipients:   rcpts,
		Raw:          raw,
		Source:       store.SourceAPI,
		ClientIP:     ip,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sendResponse{ID: sum.ID})
}

// composeMessage builds a MIME message. Bcc recipients are deliberately not
// written to the headers.
func composeMessage(from *mail.Address, to, cc []*mail.Address, subject, text, html string, now time.Time) ([]byte, error) {
	var h gomail.Header
	h.SetDate(now)
	h.SetAddressList("From", []*gomail.Address{from})
	if len(to) > 0 {
		h.SetAddressList("To", to)
	}
	if len(cc) > 0 {
		h.SetAddressList("Cc", cc)
	}
	h.SetSubject(subject)
	if err := h.GenerateMessageIDWithHostname("envry.local"); err != nil {
		return nil, fmt.Errorf("generate Message-ID: %w", err)
	}

	var buf bytes.Buffer
	if html == "" {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, text); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
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
	for _, part := range []struct{ mediaType, body string }{{"text/plain", text}, {"text/html", html}} {
		var ph gomail.InlineHeader
		ph.SetContentType(part.mediaType, map[string]string{"charset": "utf-8"})
		ph.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := iw.CreatePart(ph)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(pw, part.body); err != nil {
			return nil, err
		}
		if err := pw.Close(); err != nil {
			return nil, err
		}
	}
	if err := iw.Close(); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
