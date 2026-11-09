package emulate

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/compose"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// Amazon SES: the v2 JSON API's SendEmail (POST /v2/email/outbound-emails)
// and the v1 query API's SendEmail and SendRawEmail (POST /, which Laravel's
// ses driver and older SDKs use). Any credentials and region are accepted;
// the SDKs take the base URL through their endpoint option.

type sesContent struct {
	Data    string `json:"Data"`
	Charset string `json:"Charset"`
}

type sesV2Request struct {
	FromEmailAddress string `json:"FromEmailAddress"`
	Destination      *struct {
		ToAddresses  []string `json:"ToAddresses"`
		CcAddresses  []string `json:"CcAddresses"`
		BccAddresses []string `json:"BccAddresses"`
	} `json:"Destination"`
	ReplyToAddresses []string `json:"ReplyToAddresses"`
	Content          struct {
		Simple *struct {
			Subject sesContent `json:"Subject"`
			Body    struct {
				Text *sesContent `json:"Text"`
				HTML *sesContent `json:"Html"`
			} `json:"Body"`
			Headers []struct {
				Name  string `json:"Name"`
				Value string `json:"Value"`
			} `json:"Headers"`
			Attachments []struct {
				RawContent         []byte `json:"RawContent"`
				FileName           string `json:"FileName"`
				ContentType        string `json:"ContentType"`
				ContentID          string `json:"ContentId"`
				ContentDisposition string `json:"ContentDisposition"`
			} `json:"Attachments"`
		} `json:"Simple"`
		Raw *struct {
			Data []byte `json:"Data"`
		} `json:"Raw"`
		Template *struct {
			TemplateName string `json:"TemplateName"`
			TemplateArn  string `json:"TemplateArn"`
			TemplateData string `json:"TemplateData"`
		} `json:"Template"`
	} `json:"Content"`
	EmailTags []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"EmailTags"`
}

func (s *Server) sesRoutes(r chi.Router) {
	r.Post("/v2/email/outbound-emails", s.sesV2Send)
	r.Post("/", s.sesV1)
}

func (s *Server) sesV2Send(w http.ResponseWriter, r *http.Request) {
	var req sesV2Request
	if err := decodeJSON(r, &req); err != nil {
		s.sesV2Error(w, err)
		return
	}
	var to, cc, bcc []string
	if d := req.Destination; d != nil {
		to, cc, bcc = d.ToAddresses, d.CcAddresses, d.BccAddresses
	}
	var id string
	var err error
	c := req.Content
	switch {
	case c.Raw != nil:
		id, err = s.sesRaw(r, c.Raw.Data, req.FromEmailAddress, append(append(to, cc...), bcc...))
	case c.Simple != nil || c.Template != nil:
		m, merr := sesMessage(req.FromEmailAddress, to, cc, bcc, req.ReplyToAddresses)
		if merr != nil {
			s.sesV2Error(w, merr)
			return
		}
		if simple := c.Simple; simple != nil {
			m.Subject = simple.Subject.Data
			if simple.Body.Text != nil {
				m.Text = simple.Body.Text.Data
			}
			if simple.Body.HTML != nil {
				m.HTML = simple.Body.HTML.Data
			}
			for _, h := range simple.Headers {
				m.Headers = append(m.Headers, compose.Header{Name: h.Name, Value: h.Value})
			}
			for _, a := range simple.Attachments {
				f := compose.Attachment{Filename: a.FileName, ContentType: a.ContentType, Data: a.RawContent}
				if strings.EqualFold(a.ContentDisposition, "INLINE") {
					f.ContentID = a.ContentID
				}
				m.Attachments = append(m.Attachments, f)
			}
		} else {
			t := c.Template
			name := t.TemplateName
			if name == "" {
				name = t.TemplateArn
			}
			var data any
			_ = json.Unmarshal([]byte(t.TemplateData), &data)
			m.Subject = "SES template " + name
			m.Text = templateNote("Amazon SES", name, data)
		}
		for _, tag := range req.EmailTags {
			m.Headers = append(m.Headers, compose.Header{Name: "X-SES-Tag", Value: tag.Name + "=" + tag.Value})
		}
		id, err = s.deliver(r.Context(), r, SES, m)
	default:
		err = invalid("Content is required: Simple, Raw or Template.")
	}
	if err != nil {
		s.sesV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"MessageId": id})
}

// sesMessage starts a message from SES's address fields.
func sesMessage(from string, to, cc, bcc, replyTo []string) (compose.Message, error) {
	if from == "" {
		return compose.Message{}, invalid("Missing required parameter: FromEmailAddress.")
	}
	f, err := address("FromEmailAddress", from)
	if err != nil {
		return compose.Message{}, invalid("Invalid address: %s.", from)
	}
	m := compose.Message{From: f}
	for _, list := range []struct {
		in  []string
		out *[]*mail.Address
	}{{to, &m.To}, {cc, &m.Cc}, {bcc, &m.Bcc}, {replyTo, &m.ReplyTo}} {
		if *list.out, err = addresses("Destination", list.in); err != nil {
			return compose.Message{}, invalid("Invalid address in Destination.")
		}
	}
	if len(m.Recipients()) == 0 {
		return compose.Message{}, invalid("Missing required parameter: Destination.")
	}
	return m, nil
}

// sesRaw captures a raw message. Without explicit recipients, the To, Cc
// and Bcc headers say where it goes; without a sender, the From header.
func (s *Server) sesRaw(r *http.Request, raw []byte, from string, rcpts []string) (string, error) {
	if len(raw) == 0 {
		return "", invalid("RawMessage is required.")
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		return "", invalid("Could not parse the raw message: %v", err)
	}
	if from == "" {
		from = p.From.Address
	} else if a, err := mail.ParseAddress(from); err == nil {
		from = a.Address
	}
	var envelope []string
	for _, rc := range rcpts {
		if a, err := mail.ParseAddress(rc); err == nil {
			envelope = append(envelope, a.Address)
		}
	}
	if len(envelope) == 0 {
		for _, a := range append(append(append([]mimeparse.Address{}, p.To...), p.Cc...), headerAddresses(p, "Bcc")...) {
			envelope = append(envelope, a.Address)
		}
	}
	if len(envelope) == 0 {
		return "", invalid("Missing final '@domain'")
	}
	return s.capture(r.Context(), r, SES, from, envelope, raw)
}

func headerAddresses(p *mimeparse.Message, name string) []mimeparse.Address {
	var out []mimeparse.Address
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			out = append(out, mimeparse.ParseAddressList(h.Value)...)
		}
	}
	return out
}

// sesV2Error answers like the v2 API: the error type in a header and a
// JSON message.
func (s *Server) sesV2Error(w http.ResponseWriter, err error) {
	if isInvalid(err) {
		w.Header().Set("X-Amzn-Errortype", "BadRequestException")
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	s.log.Error("ses emulation", "err", err)
	w.Header().Set("X-Amzn-Errortype", "InternalFailure")
	writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
}

// ---- v1 query API ----

const sesNamespace = "http://ses.amazonaws.com/doc/2010-12-01/"

func (s *Server) sesV1(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.sesV1Error(w, invalid("Could not read the request: %v", err))
		return
	}
	f := r.PostForm
	members := func(prefix string) []string {
		var out []string
		for i := 1; ; i++ {
			v := f.Get(fmt.Sprintf("%s.member.%d", prefix, i))
			if v == "" {
				return out
			}
			out = append(out, v)
		}
	}

	action := f.Get("Action")
	var id string
	var err error
	switch action {
	case "SendRawEmail":
		var raw []byte
		if raw, err = decodeBase64(f.Get("RawMessage.Data")); err != nil {
			err = invalid("RawMessage.Data is not valid base64.")
			break
		}
		id, err = s.sesRaw(r, raw, f.Get("Source"), members("Destinations"))
	case "SendEmail":
		var m compose.Message
		m, err = sesMessage(f.Get("Source"), members("Destination.ToAddresses"), members("Destination.CcAddresses"),
			members("Destination.BccAddresses"), members("ReplyToAddresses"))
		if err != nil {
			break
		}
		m.Subject = f.Get("Message.Subject.Data")
		m.Text = f.Get("Message.Body.Text.Data")
		m.HTML = f.Get("Message.Body.Html.Data")
		id, err = s.deliver(r.Context(), r, SES, m)
	default:
		err = errUnsupportedAction{action}
	}
	if err != nil {
		s.sesV1Error(w, err)
		return
	}

	// <SendEmailResponse><SendEmailResult><MessageId>, named after the action.
	type result struct {
		XMLName   xml.Name
		MessageID string `xml:"MessageId"`
	}
	type response struct {
		XMLName  xml.Name
		Xmlns    string `xml:"xmlns,attr"`
		Result   result
		Metadata struct {
			RequestID string `xml:"RequestId"`
		} `xml:"ResponseMetadata"`
	}
	resp := response{
		XMLName: xml.Name{Local: action + "Response"},
		Xmlns:   sesNamespace,
		Result:  result{XMLName: xml.Name{Local: action + "Result"}, MessageID: id},
	}
	resp.Metadata.RequestID = id
	writeXML(w, http.StatusOK, resp)
}

type errUnsupportedAction struct{ action string }

func (e errUnsupportedAction) Error() string {
	return fmt.Sprintf("Envry emulates SES SendEmail and SendRawEmail, not %q.", e.action)
}

// sesV1Error answers like the query API: an XML ErrorResponse.
func (s *Server) sesV1Error(w http.ResponseWriter, err error) {
	code, status := "InvalidParameterValue", http.StatusBadRequest
	var unsupported errUnsupportedAction
	switch {
	case errors.As(err, &unsupported):
		code = "InvalidAction"
	case !isInvalid(err):
		s.log.Error("ses emulation", "err", err)
		code, status = "InternalFailure", http.StatusInternalServerError
	}
	type errorResponse struct {
		XMLName xml.Name `xml:"ErrorResponse"`
		Xmlns   string   `xml:"xmlns,attr"`
		Error   struct {
			Type    string `xml:"Type"`
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		} `xml:"Error"`
		RequestID string `xml:"RequestId"`
	}
	var e errorResponse
	e.Xmlns = sesNamespace
	e.Error.Type, e.Error.Code, e.Error.Message = "Sender", code, err.Error()
	e.RequestID = "envry"
	writeXML(w, status, e)
}

// writeXML writes v as an XML document.
func writeXML(w http.ResponseWriter, status int, v any) {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(b)
}
