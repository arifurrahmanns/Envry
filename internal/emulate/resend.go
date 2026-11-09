package emulate

import (
	"net/http"
	"net/mail"
	"path"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
)

// Resend: POST /emails and /emails/batch (https://resend.com/docs/api-reference).
// The Node SDK reads its base URL from RESEND_BASE_URL.

type resendEmail struct {
	From        string             `json:"from"`
	To          stringList         `json:"to"`
	Cc          stringList         `json:"cc"`
	Bcc         stringList         `json:"bcc"`
	ReplyTo     stringList         `json:"reply_to"`
	Subject     string             `json:"subject"`
	HTML        string             `json:"html"`
	Text        string             `json:"text"`
	Headers     map[string]string  `json:"headers"`
	Attachments []resendAttachment `json:"attachments"`
	Tags        []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"tags"`
}

type resendAttachment struct {
	Filename    string      `json:"filename"`
	Content     fileContent `json:"content"`
	Path        string      `json:"path"`
	ContentType string      `json:"content_type"`
	ContentID   string      `json:"content_id"`
}

type resendID struct {
	ID string `json:"id"`
}

func (s *Server) resendRoutes(r chi.Router) {
	r.Post("/emails", func(w http.ResponseWriter, r *http.Request) {
		var req resendEmail
		if err := decodeJSON(r, &req); err != nil {
			s.resendError(w, err)
			return
		}
		id, err := s.resendSend(r, req)
		if err != nil {
			s.resendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resendID{ID: id})
	})
	r.Post("/emails/batch", func(w http.ResponseWriter, r *http.Request) {
		var req []resendEmail
		if err := decodeJSON(r, &req); err != nil {
			s.resendError(w, err)
			return
		}
		// Resend checks the whole batch before sending any of it.
		for _, e := range req {
			if _, err := resendMessage(e); err != nil {
				s.resendError(w, err)
				return
			}
		}
		out := struct {
			Data []resendID `json:"data"`
		}{Data: []resendID{}}
		for _, e := range req {
			id, err := s.resendSend(r, e)
			if err != nil {
				s.resendError(w, err)
				return
			}
			out.Data = append(out.Data, resendID{ID: id})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (s *Server) resendSend(r *http.Request, e resendEmail) (string, error) {
	m, err := resendMessage(e)
	if err != nil {
		return "", err
	}
	return s.deliver(r.Context(), r, Resend, m)
}

func resendMessage(e resendEmail) (compose.Message, error) {
	if e.From == "" {
		return compose.Message{}, invalid("Missing `from` field.")
	}
	if len(e.To) == 0 {
		return compose.Message{}, invalid("Missing `to` field.")
	}
	from, err := address("from", e.From)
	if err != nil {
		return compose.Message{}, err
	}
	m := compose.Message{From: from, Subject: e.Subject, HTML: e.HTML, Text: e.Text}
	for _, f := range []struct {
		name string
		in   stringList
		out  *[]*mail.Address
	}{{"to", e.To, &m.To}, {"cc", e.Cc, &m.Cc}, {"bcc", e.Bcc, &m.Bcc}, {"reply_to", e.ReplyTo, &m.ReplyTo}} {
		if *f.out, err = addresses(f.name, f.in); err != nil {
			return compose.Message{}, err
		}
	}
	m.Headers = headerList(e.Headers)
	for _, t := range e.Tags {
		m.Headers = append(m.Headers, compose.Header{Name: "X-Resend-Tag", Value: t.Name + "=" + t.Value})
	}
	for _, a := range e.Attachments {
		name := a.Filename
		if name == "" && a.Path != "" {
			name = path.Base(a.Path)
		}
		// Attachments given by URL are listed but not downloaded: Envry
		// makes no requests on an app's behalf.
		m.Attachments = append(m.Attachments, compose.Attachment{
			Filename: name, ContentType: a.ContentType, Data: a.Content, ContentID: a.ContentID,
		})
	}
	return m, nil
}

// resendError answers like Resend: {"statusCode":422,"name":"validation_error","message":...}.
func (s *Server) resendError(w http.ResponseWriter, err error) {
	if isInvalid(err) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"statusCode": http.StatusUnprocessableEntity, "name": "validation_error", "message": err.Error(),
		})
		return
	}
	s.log.Error("resend emulation", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"statusCode": http.StatusInternalServerError, "name": "internal_server_error", "message": err.Error(),
	})
}
