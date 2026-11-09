package api

import (
	"errors"
	"net/http"

	"github.com/arifurrahmanns/envry/internal/emulate"
)

// Provider webhooks: where Envry sends delivery events (delivered,
// bounced, complained) for mail sent through an emulated provider API.

// SetWebhooks connects the provider webhook settings. Call before serving.
func (s *Server) SetWebhooks(w *emulate.Webhooks) { s.webhooks = w }

type webhookSaveRequest struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	// Secret replaces Resend's signing secret or Mailgun's signing key;
	// omit it to keep the current one.
	Secret *string `json:"secret"`
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	items, err := s.webhooks.Configs(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemsDTO[emulate.WebhookConfig]{Items: items})
}

func (s *Server) handleSaveWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookSaveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.webhooks.Save(r.Context(), param(r, "provider"), req.URL, req.Enabled, req.Secret)
	if err != nil {
		s.webhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleTestWebhook sends a made-up "delivered" event and returns how the
// app answered. It is a POST because it calls the configured URL.
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	d, err := s.webhooks.SendTest(r.Context(), param(r, "provider"))
	if err != nil {
		s.webhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, itemsDTO[emulate.Delivery]{Items: s.webhooks.Deliveries()})
}

func (s *Server) webhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, emulate.ErrUnknownProvider):
		writeError(w, http.StatusNotFound, "unknown provider")
	case emulate.IsInvalid(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.internalError(w, err)
	}
}

type itemsDTO[T any] struct {
	Items []T `json:"items"`
}
