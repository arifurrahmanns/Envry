package emulate

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
)

// SendGrid: POST /v3/mail/send (https://www.twilio.com/docs/sendgrid/api-reference/mail-send).
// Each personalization is its own email. The Node SDK takes a base URL
// through @sendgrid/client's setDefaultRequest("baseUrl", ...).

type sgAddress struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type sgPersonalization struct {
	To                  []sgAddress       `json:"to"`
	Cc                  []sgAddress       `json:"cc"`
	Bcc                 []sgAddress       `json:"bcc"`
	From                *sgAddress        `json:"from"`
	Subject             string            `json:"subject"`
	Headers             map[string]string `json:"headers"`
	Substitutions       map[string]string `json:"substitutions"`
	DynamicTemplateData map[string]any    `json:"dynamic_template_data"`
	CustomArgs          map[string]string `json:"custom_args"`
}

type sgMail struct {
	Personalizations []sgPersonalization `json:"personalizations"`
	From             *sgAddress          `json:"from"`
	ReplyTo          *sgAddress          `json:"reply_to"`
	ReplyToList      []sgAddress         `json:"reply_to_list"`
	Subject          string              `json:"subject"`
	Content          []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"content"`
	Attachments []struct {
		Content     string `json:"content"`
		Type        string `json:"type"`
		Filename    string `json:"filename"`
		Disposition string `json:"disposition"`
		ContentID   string `json:"content_id"`
	} `json:"attachments"`
	TemplateID string            `json:"template_id"`
	Headers    map[string]string `json:"headers"`
	Categories []string          `json:"categories"`
	CustomArgs map[string]string `json:"custom_args"`
}

func (s *Server) sendgridRoutes(r chi.Router) {
	r.Post("/v3/mail/send", func(w http.ResponseWriter, r *http.Request) {
		var req sgMail
		if err := decodeJSON(r, &req); err != nil {
			s.sendgridError(w, err, "")
			return
		}
		msgs, metas, field, err := sendgridMessages(req)
		if err != nil {
			s.sendgridError(w, err, field)
			return
		}
		var first string
		for i, m := range msgs {
			id, err := s.deliver(r.Context(), r, SendGrid, m, metas[i])
			if err != nil {
				s.sendgridError(w, err, "")
				return
			}
			if first == "" {
				first = id
			}
		}
		// SendGrid answers 202 Accepted with an empty body.
		w.Header().Set("X-Message-Id", first)
		w.WriteHeader(http.StatusAccepted)
	})
}

// sendgridMessages returns one message per personalization with what its
// events echo (categories and custom arguments), or the error and the
// field it is about.
func sendgridMessages(req sgMail) ([]compose.Message, []eventMeta, string, error) {
	if len(req.Personalizations) == 0 {
		return nil, nil, "personalizations", invalid("The personalizations field is required and must have at least one personalization.")
	}
	var text, html string
	for _, c := range req.Content {
		switch strings.ToLower(c.Type) {
		case "text/plain":
			text = c.Value
		case "text/html":
			html = c.Value
		}
	}
	if text == "" && html == "" && req.TemplateID == "" {
		return nil, nil, "content", invalid("Unless a valid template_id is provided, the content parameter is required.")
	}

	var replyTo []*mail.Address
	if req.ReplyTo != nil && req.ReplyTo.Email != "" {
		replyTo = append(replyTo, &mail.Address{Name: req.ReplyTo.Name, Address: req.ReplyTo.Email})
	}
	for _, a := range req.ReplyToList {
		replyTo = append(replyTo, &mail.Address{Name: a.Name, Address: a.Email})
	}
	var files []compose.Attachment
	for i, a := range req.Attachments {
		data, err := decodeBase64(a.Content)
		if err != nil {
			return nil, nil, fmt.Sprintf("attachments.%d.content", i), invalid("The attachment content must be base64 encoded.")
		}
		f := compose.Attachment{Filename: a.Filename, ContentType: a.Type, Data: data}
		if strings.EqualFold(a.Disposition, "inline") {
			f.ContentID = a.ContentID
		}
		files = append(files, f)
	}
	var categories []compose.Header
	for _, c := range req.Categories {
		categories = append(categories, compose.Header{Name: "X-SendGrid-Category", Value: c})
	}

	var out []compose.Message
	var metas []eventMeta
	for i, p := range req.Personalizations {
		from := req.From
		if p.From != nil && p.From.Email != "" {
			from = p.From
		}
		if from == nil || from.Email == "" {
			return nil, nil, "from.email", invalid("The from email does not contain a valid address.")
		}
		if len(p.To) == 0 {
			return nil, nil, fmt.Sprintf("personalizations.%d.to", i), invalid("The to array is required for all personalization objects, and must have at least one email object with a valid email address.")
		}
		fromAddr, err := address("from", sgFormat(*from))
		if err != nil {
			return nil, nil, "from.email", invalid("The from email does not contain a valid address.")
		}
		m := compose.Message{From: fromAddr, ReplyTo: replyTo, Attachments: files}
		for _, list := range []struct {
			field string
			in    []sgAddress
			out   *[]*mail.Address
		}{{"to", p.To, &m.To}, {"cc", p.Cc, &m.Cc}, {"bcc", p.Bcc, &m.Bcc}} {
			for j, a := range list.in {
				parsed, err := address(list.field, sgFormat(a))
				if err != nil {
					return nil, nil, fmt.Sprintf("personalizations.%d.%s.%d.email", i, list.field, j), invalid("Does not contain a valid address.")
				}
				*list.out = append(*list.out, parsed)
			}
		}
		m.Subject = req.Subject
		if p.Subject != "" {
			m.Subject = p.Subject
		}
		m.Text, m.HTML = text, html
		if req.TemplateID != "" && text == "" && html == "" {
			m.Text = templateNote("SendGrid", req.TemplateID, p.DynamicTemplateData)
		}
		// Legacy substitutions replace their keys anywhere in the message.
		for key, value := range p.Substitutions {
			m.Subject = strings.ReplaceAll(m.Subject, key, value)
			m.Text = strings.ReplaceAll(m.Text, key, value)
			m.HTML = strings.ReplaceAll(m.HTML, key, value)
		}
		headers := map[string]string{}
		for k, v := range req.Headers {
			headers[k] = v
		}
		for k, v := range p.Headers {
			headers[k] = v
		}
		m.Headers = append(headerList(headers), categories...)
		out = append(out, m)
		args := map[string]string{}
		for k, v := range req.CustomArgs {
			args[k] = v
		}
		for k, v := range p.CustomArgs {
			args[k] = v
		}
		metas = append(metas, eventMeta{tags: req.Categories, values: args})
	}
	return out, metas, "", nil
}

func sgFormat(a sgAddress) string {
	if a.Name == "" {
		return a.Email
	}
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// sendgridError answers like SendGrid: {"errors":[{"message","field","help"}]}.
func (s *Server) sendgridError(w http.ResponseWriter, err error, field string) {
	if isInvalid(err) {
		var f any
		if field != "" {
			f = field
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []map[string]any{{"message": err.Error(), "field": f, "help": nil}},
		})
		return
	}
	s.log.Error("sendgrid emulation", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"errors": []map[string]any{{"message": err.Error(), "field": nil, "help": nil}},
	})
}
