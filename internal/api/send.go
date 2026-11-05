package api

import (
	"net"
	"net/http"
	"net/mail"

	"github.com/arifurrahmanns/envry/internal/compose"
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
		writeError(w, http.StatusBadRequest, "invalid from address: "+req.From)
		return
	}
	msg := compose.Message{From: from, Subject: req.Subject, Text: req.Text, HTML: req.HTML}
	for _, list := range []struct {
		in  []string
		out *[]*mail.Address
	}{{req.To, &msg.To}, {req.Cc, &msg.Cc}, {req.Bcc, &msg.Bcc}} {
		if *list.out, err = compose.ParseAddresses(list.in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid recipient: "+err.Error())
			return
		}
	}
	if len(msg.Recipients()) == 0 {
		writeError(w, http.StatusBadRequest, "at least one recipient is required")
		return
	}

	raw, err := compose.Build(msg)
	if err != nil {
		s.internalError(w, err)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	sum, err := s.store.Ingest(r.Context(), store.IngestInput{
		EnvelopeFrom: from.Address,
		Recipients:   msg.Recipients(),
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
