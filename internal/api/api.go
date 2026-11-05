// Package api implements the JSON REST API under /api/v1 and the /ws event
// stream used by the web UI and automated tests.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/store"
	"github.com/arifurrahmanns/envry/internal/version"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	cfg   *config.Config
	store *store.Store
	bus   *events.Bus
	log   *slog.Logger

	closeOnce sync.Once
	closing   chan struct{}
}

// New returns an API server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, bus: st.Bus(), log: log, closing: make(chan struct{})}
}

// Close ends long-lived requests (/ws, /wait) so the HTTP server can shut
// down promptly.
func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.closing) })
}

// Routes returns the router to mount at /api.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requireJSONBody)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	r.Route("/v1", func(r chi.Router) {
		r.Get("/info", s.handleInfo)

		r.Get("/messages", s.handleListMessages)
		r.Delete("/messages", s.handleDeleteAllMessages)
		r.Get("/messages/{id}", s.handleGetMessage)
		r.Delete("/messages/{id}", s.handleDeleteMessage)
		r.Get("/messages/{id}/raw", s.handleRaw)
		r.Get("/messages/{id}/html", s.handleHTML)
		r.Get("/messages/{id}/attachments/{aid}", s.handleAttachment)

		r.Get("/mailboxes", s.handleListMailboxes)
		r.Get("/mailboxes/{address}", s.handleGetMailbox)
		r.Get("/mailboxes/{address}/messages", s.handleMailboxMessages)

		r.Post("/accounts", s.handleCreateAccount)
		r.Get("/accounts/{address}", s.handleGetAccount)
		r.Delete("/accounts/{address}", s.handleDeleteAccount)

		r.Patch("/deliveries/{id}", s.handleUpdateDelivery)
		r.Delete("/deliveries/{id}", s.handleDeleteDelivery)

		r.Post("/send", s.handleSend)
		r.Get("/wait", s.handleWait)
	})
	return r
}

type portsDTO struct {
	SMTP int `json:"smtp"`
	POP3 int `json:"pop3"`
	IMAP int `json:"imap"`
	HTTP int `json:"http"`
}

type settingsDTO struct {
	MaxMessages int   `json:"maxMessages"`
	MaxSize     int64 `json:"maxSize"`
	StrictAuth  bool  `json:"strictAuth"`
}

type infoDTO struct {
	Version  string      `json:"version"`
	Commit   string      `json:"commit"`
	Ports    portsDTO    `json:"ports"`
	Settings settingsDTO `json:"settings"`
	Stats    store.Stats `json:"stats"`
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, infoDTO{
		Version: version.Version,
		Commit:  version.Commit,
		Ports: portsDTO{
			SMTP: port(s.cfg.SMTPAddr),
			POP3: port(s.cfg.POP3Addr),
			IMAP: port(s.cfg.IMAPAddr),
			HTTP: port(s.cfg.HTTPAddr),
		},
		Settings: settingsDTO{
			MaxMessages: s.cfg.MaxMessages,
			MaxSize:     s.cfg.MaxSize,
			StrictAuth:  s.cfg.StrictAuth,
		},
		Stats: stats,
	})
}

func port(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// requireJSONBody rejects POST/PUT/PATCH requests that are not JSON. Besides
// validating input, this forces cross-site requests into a CORS preflight,
// which Envry never approves, so other websites cannot drive the API.
func requireJSONBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "request body must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

const maxJSONBody = 32 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorDTO struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorDTO{Error: msg})
}

// storeError maps store errors to HTTP responses.
func (s *Server) storeError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, what+" not found")
		return
	}
	s.internalError(w, err)
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("api error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// param returns a decoded URL path parameter. chi matches on the escaped
// path, so an address requested as dev%40test.local arrives still encoded.
func param(r *http.Request, key string) string {
	v := chi.URLParam(r, key)
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

func queryInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}

func listOptions(r *http.Request) store.ListOptions {
	q := r.URL.Query()
	return store.ListOptions{
		Query:  q.Get("q"),
		From:   q.Get("from"),
		To:     q.Get("to"),
		Before: q.Get("before"),
		Limit:  queryInt(r, "limit"),
	}
}
