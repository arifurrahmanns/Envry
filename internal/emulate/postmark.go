package emulate

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
)

// Postmark: POST /email, /email/batch and /email/withTemplate
// (https://postmarkapp.com/developer/api/email-api). The Node SDK takes the
// host (and a path) through its requestHost option, with useHttps: false.

type pmEmail struct {
	From     string `json:"From"`
	To       string `json:"To"`
	Cc       string `json:"Cc"`
	Bcc      string `json:"Bcc"`
	ReplyTo  string `json:"ReplyTo"`
	Subject  string `json:"Subject"`
	Tag      string `json:"Tag"`
	HTMLBody string `json:"HtmlBody"`
	TextBody string `json:"TextBody"`
	Headers  []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"Headers"`
	Metadata    map[string]string `json:"Metadata"`
	Attachments []struct {
		Name        string      `json:"Name"`
		Content     fileContent `json:"Content"`
		ContentType string      `json:"ContentType"`
		ContentID   string      `json:"ContentID"`
	} `json:"Attachments"`
	MessageStream string `json:"MessageStream"`

	// /email/withTemplate
	TemplateID    int64          `json:"TemplateId"`
	TemplateAlias string         `json:"TemplateAlias"`
	TemplateModel map[string]any `json:"TemplateModel"`
}

type pmResult struct {
	To          string `json:"To,omitempty"`
	SubmittedAt string `json:"SubmittedAt,omitempty"`
	MessageID   string `json:"MessageID,omitempty"`
	ErrorCode   int    `json:"ErrorCode"`
	Message     string `json:"Message"`
}

// Postmark's error codes: an invalid email request, and recipients marked
// inactive (after a hard bounce or spam complaint).
const (
	pmInvalidEmail      = 300
	pmInactiveRecipient = 406
)

func pmInactiveMessage(e errRejected) string {
	return "You tried to send to recipient(s) that have been marked as inactive. Found inactive addresses: " +
		e.address + ". (Envry failure simulation.)"
}

func (s *Server) postmarkRoutes(r chi.Router) {
	single := func(withTemplate bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req pmEmail
			if err := decodeJSON(r, &req); err != nil {
				s.postmarkError(w, err)
				return
			}
			res, err := s.postmarkSend(r, req, withTemplate, false)
			if err != nil {
				s.postmarkError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		}
	}
	r.Post("/email", single(false))
	r.Post("/email/withTemplate", single(true))
	batch := func(withTemplate bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req []pmEmail
			if withTemplate {
				var wrapped struct {
					Messages []pmEmail `json:"Messages"`
				}
				if err := decodeJSON(r, &wrapped); err != nil {
					s.postmarkError(w, err)
					return
				}
				req = wrapped.Messages
			} else if err := decodeJSON(r, &req); err != nil {
				s.postmarkError(w, err)
				return
			}
			// A rate limit refuses the whole batch; otherwise each message
			// succeeds or fails on its own, as in Postmark.
			if err := s.delay(r.Context()); err != nil {
				s.postmarkError(w, err)
				return
			}
			if err := s.throttled(); err != nil {
				s.postmarkError(w, err)
				return
			}
			out := []pmResult{}
			for _, e := range req {
				res, err := s.postmarkSend(r, e, withTemplate, true)
				var rejected errRejected
				switch {
				case err == nil:
				case errors.As(err, &rejected):
					res = pmResult{ErrorCode: pmInactiveRecipient, Message: pmInactiveMessage(rejected)}
				case isInvalid(err):
					res = pmResult{ErrorCode: pmInvalidEmail, Message: err.Error()}
				default:
					s.postmarkError(w, err)
					return
				}
				out = append(out, res)
			}
			writeJSON(w, http.StatusOK, out)
		}
	}
	r.Post("/email/batch", batch(false))
	r.Post("/email/batchWithTemplates", batch(true))
}

// postmarkSend captures one message. In a batch the delay and rate limit
// were already applied to the whole request, so only refused recipients
// are checked here.
func (s *Server) postmarkSend(r *http.Request, e pmEmail, withTemplate, inBatch bool) (pmResult, error) {
	if strings.TrimSpace(e.From) == "" {
		return pmResult{}, invalid("Invalid 'From' address: ''.")
	}
	if strings.TrimSpace(e.To+e.Cc+e.Bcc) == "" {
		return pmResult{}, invalid("Zero recipients specified")
	}
	from, err := address("From", e.From)
	if err != nil {
		return pmResult{}, invalid("Invalid 'From' address: '%s'.", e.From)
	}
	m := compose.Message{From: from, Subject: e.Subject, HTML: e.HTMLBody, Text: e.TextBody}
	if m.To, err = addresses("To", []string{e.To}); err != nil {
		return pmResult{}, invalid("Error parsing 'To': Illegal email address '%s'.", e.To)
	}
	if m.Cc, err = addresses("Cc", []string{e.Cc}); err != nil {
		return pmResult{}, invalid("Error parsing 'Cc': Illegal email address '%s'.", e.Cc)
	}
	if m.Bcc, err = addresses("Bcc", []string{e.Bcc}); err != nil {
		return pmResult{}, invalid("Error parsing 'Bcc': Illegal email address '%s'.", e.Bcc)
	}
	if m.ReplyTo, err = addresses("ReplyTo", []string{e.ReplyTo}); err != nil {
		return pmResult{}, invalid("Error parsing 'ReplyTo': Illegal email address '%s'.", e.ReplyTo)
	}
	if withTemplate {
		template := e.TemplateAlias
		if template == "" {
			template = fmt.Sprint(e.TemplateID)
		}
		m.Text, m.HTML = templateNote("Postmark", template, e.TemplateModel), ""
		if m.Subject == "" {
			m.Subject = "Postmark template " + template
		}
	} else if e.HTMLBody == "" && e.TextBody == "" {
		return pmResult{}, invalid("Provide either email TextBody or HtmlBody or both.")
	}
	for _, h := range e.Headers {
		m.Headers = append(m.Headers, compose.Header{Name: h.Name, Value: h.Value})
	}
	// Postmark adds these to the mail it delivers.
	if e.Tag != "" {
		m.Headers = append(m.Headers, compose.Header{Name: "X-PM-Tag", Value: e.Tag})
	}
	for _, h := range headerList(e.Metadata) {
		m.Headers = append(m.Headers, compose.Header{Name: "X-PM-Metadata-" + h.Name, Value: h.Value})
	}
	if e.MessageStream != "" {
		m.Headers = append(m.Headers, compose.Header{Name: "X-PM-Message-Stream", Value: e.MessageStream})
	}
	for _, a := range e.Attachments {
		m.Attachments = append(m.Attachments, compose.Attachment{
			Filename: a.Name, ContentType: a.ContentType, Data: a.Content,
			ContentID: strings.TrimPrefix(a.ContentID, "cid:"),
		})
	}

	var failure error
	if inBatch {
		failure = s.refused(recipients(m))
	} else {
		failure = s.simulate(r.Context(), recipients(m))
	}
	if failure != nil {
		return pmResult{}, failure
	}
	meta := eventMeta{values: e.Metadata, stream: e.MessageStream}
	if e.Tag != "" {
		meta.tags = []string{e.Tag}
	}
	id, err := s.deliver(r.Context(), r, Postmark, m, meta)
	if err != nil {
		return pmResult{}, err
	}
	return pmResult{
		To:          e.To,
		SubmittedAt: time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z"),
		MessageID:   id,
		ErrorCode:   0,
		Message:     "OK",
	}, nil
}

// postmarkError answers like Postmark: 422 {"ErrorCode":300,"Message":...}.
func (s *Server) postmarkError(w http.ResponseWriter, err error) {
	var rejected errRejected
	switch {
	case errors.As(err, &rejected):
		writeJSON(w, http.StatusUnprocessableEntity, pmResult{ErrorCode: pmInactiveRecipient, Message: pmInactiveMessage(rejected)})
		return
	case errors.As(err, new(errThrottled)):
		writeJSON(w, http.StatusTooManyRequests, pmResult{ErrorCode: 0, Message: err.Error()})
		return
	}
	if isInvalid(err) {
		writeJSON(w, http.StatusUnprocessableEntity, pmResult{ErrorCode: pmInvalidEmail, Message: err.Error()})
		return
	}
	s.log.Error("postmark emulation", "err", err)
	writeJSON(w, http.StatusInternalServerError, pmResult{ErrorCode: 500, Message: err.Error()})
}
