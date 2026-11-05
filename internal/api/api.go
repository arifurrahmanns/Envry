// Package api implements the JSON REST API under /api/v1.
package api

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/version"
)

// Server holds the dependencies shared by all API handlers.
type Server struct {
	cfg *config.Config
	log *slog.Logger
}

// New returns an API server.
func New(cfg *config.Config, log *slog.Logger) *Server {
	return &Server{cfg: cfg, log: log}
}

// Routes returns the router to mount at /api.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	r.Route("/v1", func(r chi.Router) {
		r.Get("/info", s.handleInfo)
	})
	return r
}

type portsDTO struct {
	SMTP int `json:"smtp"`
	POP3 int `json:"pop3"`
	IMAP int `json:"imap"`
	HTTP int `json:"http"`
}

type infoDTO struct {
	Version string   `json:"version"`
	Commit  string   `json:"commit"`
	Ports   portsDTO `json:"ports"`
}

func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, infoDTO{
		Version: version.Version,
		Commit:  version.Commit,
		Ports: portsDTO{
			SMTP: port(s.cfg.SMTPAddr),
			POP3: port(s.cfg.POP3Addr),
			IMAP: port(s.cfg.IMAPAddr),
			HTTP: port(s.cfg.HTTPAddr),
		},
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
