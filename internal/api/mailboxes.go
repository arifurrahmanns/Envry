package api

import (
	"net/http"
	"net/mail"
	"strconv"

	"github.com/arifurrahmanns/envry/internal/store"
)

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMailboxes(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleGetMailbox(w http.ResponseWriter, r *http.Request) {
	mb, err := s.store.GetMailbox(r.Context(), param(r, "address"))
	if err != nil {
		s.storeError(w, err, "mailbox")
		return
	}
	writeJSON(w, http.StatusOK, mb)
}

func (s *Server) handleMailboxMessages(w http.ResponseWriter, r *http.Request) {
	address := param(r, "address")
	if _, err := s.store.GetMailbox(r.Context(), address); err != nil {
		s.storeError(w, err, "mailbox")
		return
	}
	page, err := s.store.MailboxMessages(r.Context(), address, r.URL.Query().Get("folder"), listOptions(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type createAccountRequest struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	addr, err := mail.ParseAddress(req.Address)
	if err != nil || addr.Name != "" {
		writeError(w, http.StatusBadRequest, "address must be a plain email address such as dev@test.local")
		return
	}
	mb, created, err := s.store.EnsureAccount(r.Context(), store.AccountInput{
		Address: addr.Address, DisplayName: req.DisplayName, Password: req.Password,
	})
	if err != nil {
		s.internalError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, mb)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	mb, err := s.store.GetMailbox(r.Context(), param(r, "address"))
	if err != nil {
		s.storeError(w, err, "account")
		return
	}
	if !mb.IsAccount {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	writeJSON(w, http.StatusOK, mb)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveAccount(r.Context(), param(r, "address")); err != nil {
		s.storeError(w, err, "account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func deliveryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(param(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "delivery not found")
		return 0, false
	}
	return id, true
}

func (s *Server) handleUpdateDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := deliveryID(w, r)
	if !ok {
		return
	}
	var patch store.DeliveryPatch
	if !decodeJSON(w, r, &patch) {
		return
	}
	d, err := s.store.UpdateDelivery(r.Context(), id, patch)
	if err != nil {
		s.storeError(w, err, "delivery")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := deliveryID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteDelivery(r.Context(), id); err != nil {
		s.storeError(w, err, "delivery")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
