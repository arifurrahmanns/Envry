// Package app wires the store, event bus and protocol servers together and
// runs them as one process. Integration tests use it to start a full server
// on random ports (pass "127.0.0.1:0" addresses and read Addrs()).
package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arifurrahmanns/envry/internal/api"
	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/imapd"
	"github.com/arifurrahmanns/envry/internal/pop3d"
	"github.com/arifurrahmanns/envry/internal/smtpd"
	"github.com/arifurrahmanns/envry/internal/store"
	"github.com/arifurrahmanns/envry/internal/tlsutil"
	"github.com/arifurrahmanns/envry/internal/webui"
)

const shutdownTimeout = 5 * time.Second

// App is one running Envry instance.
type App struct {
	cfg   *config.Config
	log   *slog.Logger
	store *store.Store
	api   *api.Server
	smtp  *smtpd.Server
	pop3  *pop3d.Server
	imap  *imapd.Server
	tls   *tlsutil.Result // nil unless --tls

	httpServer *http.Server
	endpoints  []*endpoint
}

// ListenError reports an endpoint whose address could not be bound, for
// example because another program already uses the port.
type ListenError struct {
	Service string // smtp, pop3, imap, smtps, pop3s, imaps or http
	Addr    string
	Err     error
}

func (e *ListenError) Error() string {
	return fmt.Sprintf("listen %s %s: %v", e.Service, e.Addr, e.Err)
}

func (e *ListenError) Unwrap() error { return e.Err }

// endpoint is one listening socket and the server behind it.
type endpoint struct {
	name  string
	addr  string
	tls   bool // wrap the listener in implicit TLS
	serve func(net.Listener) error
	ln    net.Listener
}

// Addrs are the actual bound addresses (useful when a port was 0). The
// TLS ones are empty unless TLS is enabled.
type Addrs struct {
	HTTP, SMTP, POP3, IMAP string
	SMTPS, POP3S, IMAPS    string
}

// New opens the store, loads TLS material and builds the servers. It does
// not open sockets.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	st, err := store.Open(ctx, store.Options{
		Path:        cfg.DBPath,
		MaxMessages: cfg.MaxMessages,
		Logger:      log,
	})
	if err != nil {
		return nil, err
	}
	for _, acct := range cfg.Accounts {
		if _, _, err := st.EnsureAccount(ctx, store.AccountInput{Address: acct.Address, Password: acct.Password}); err != nil {
			st.Close()
			return nil, fmt.Errorf("account %s: %w", acct.Address, err)
		}
	}

	a := &App{cfg: cfg, log: log, store: st, api: api.New(cfg, st, log)}

	var tlsConfig *tls.Config
	if cfg.TLS {
		a.tls, err = tlsutil.Load(tlsutil.Options{CertFile: cfg.TLSCert, KeyFile: cfg.TLSKey, Dir: tlsDir(cfg.DBPath)})
		if err != nil {
			st.Close()
			return nil, err
		}
		tlsConfig = a.tls.Config
		where := a.tls.Path
		if where == "" {
			where = "memory"
		}
		log.Info("tls enabled", "certificate", where, "self_signed", a.tls.SelfSigned, "generated", a.tls.Generated,
			"expires", a.tls.NotAfter.Format(time.DateOnly), "sha256", a.tls.Fingerprint)
		a.api.SetTLS(api.TLSInfo{SelfSigned: a.tls.SelfSigned, Fingerprint: a.tls.Fingerprint, Expires: a.tls.NotAfter})
	}

	a.smtp = smtpd.New(st, smtpd.Options{
		MaxMessageBytes:  cfg.MaxSize,
		StrictAuth:       cfg.StrictAuth,
		TLSConfig:        tlsConfig,
		Logger:           log,
		RejectRecipients: cfg.SMTPRejectRcpt,
		RejectRate:       cfg.SMTPRejectRate,
		Latency:          cfg.SMTPLatency,
	})
	if len(cfg.SMTPRejectRcpt) > 0 || cfg.SMTPRejectRate > 0 || cfg.SMTPLatency > 0 {
		log.Warn("smtp failure simulation enabled", "reject_rcpt", cfg.SMTPRejectRcpt,
			"reject_rate", cfg.SMTPRejectRate, "latency", cfg.SMTPLatency)
	}
	a.pop3 = pop3d.New(st, pop3d.Options{
		StrictAuth: cfg.StrictAuth,
		TLSConfig:  tlsConfig,
		Logger:     log,
	})
	a.imap = imapd.New(st, imapd.Options{
		StrictAuth:      cfg.StrictAuth,
		MaxMessageBytes: cfg.MaxSize,
		TLSConfig:       tlsConfig,
		Logger:          log,
	})
	a.httpServer = &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	serveHTTP := func(ln net.Listener) error {
		if err := a.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	a.endpoints = []*endpoint{
		{name: "smtp", addr: cfg.SMTPAddr, serve: a.smtp.Serve},
		{name: "pop3", addr: cfg.POP3Addr, serve: a.pop3.Serve},
		{name: "imap", addr: cfg.IMAPAddr, serve: a.imap.Serve},
	}
	if cfg.TLS {
		a.endpoints = append(a.endpoints,
			&endpoint{name: "smtps", addr: cfg.SMTPSAddr, tls: true, serve: a.smtp.Serve},
			&endpoint{name: "pop3s", addr: cfg.POP3SAddr, tls: true, serve: a.pop3.Serve},
			&endpoint{name: "imaps", addr: cfg.IMAPSAddr, tls: true, serve: a.imap.Serve},
		)
	}
	a.endpoints = append(a.endpoints, &endpoint{name: "http", addr: cfg.HTTPAddr, serve: serveHTTP})
	return a, nil
}

// tlsDir keeps a generated certificate next to the database so mail
// clients only have to trust it once. An in-memory database gets an
// in-memory certificate.
func tlsDir(dbPath string) string {
	if dbPath == ":memory:" {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), "tls")
}

// Store exposes the store, mainly for tests.
func (a *App) Store() *store.Store { return a.store }

// TLS returns the loaded certificate, or nil when TLS is off.
func (a *App) TLS() *tlsutil.Result { return a.tls }

func (a *App) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(a.log))
	if a.cfg.UIAuth != nil {
		r.Use(basicAuth(a.cfg.UIAuth))
	}
	r.Mount("/api", a.api.Routes())
	r.Get("/ws", a.api.HandleWS)

	ui := webui.New()
	if !ui.Built() {
		a.log.Warn("web UI not embedded in this build; serving a placeholder page (run `make build`)")
	}
	r.Handle("/*", ui)
	return r
}

// Listen binds every listener. Call it before Serve, or use Run.
func (a *App) Listen() error {
	for i, e := range a.endpoints {
		ln, err := net.Listen("tcp", e.addr)
		if err != nil {
			for _, prev := range a.endpoints[:i] {
				prev.ln.Close()
				prev.ln = nil
			}
			return &ListenError{Service: e.name, Addr: e.addr, Err: err}
		}
		if e.tls {
			ln = tls.NewListener(ln, a.tls.Config)
		}
		e.ln = ln
	}
	a.api.SetPorts(a.boundPorts())
	return nil
}

func (a *App) boundPorts() api.Ports {
	addrs := a.Addrs()
	p := func(addr string) int {
		if addr == "" {
			return 0
		}
		_, port, _ := net.SplitHostPort(addr)
		n, _ := strconv.Atoi(port)
		return n
	}
	return api.Ports{
		SMTP: p(addrs.SMTP), POP3: p(addrs.POP3), IMAP: p(addrs.IMAP), HTTP: p(addrs.HTTP),
		SMTPS: p(addrs.SMTPS), POP3S: p(addrs.POP3S), IMAPS: p(addrs.IMAPS),
	}
}

// Addrs returns the bound addresses. Valid after Listen.
func (a *App) Addrs() Addrs {
	var out Addrs
	fields := map[string]*string{
		"http": &out.HTTP, "smtp": &out.SMTP, "pop3": &out.POP3, "imap": &out.IMAP,
		"smtps": &out.SMTPS, "pop3s": &out.POP3S, "imaps": &out.IMAPS,
	}
	for _, e := range a.endpoints {
		if e.ln != nil {
			*fields[e.name] = e.ln.Addr().String()
		}
	}
	return out
}

// Serve runs until ctx is cancelled or a server fails, then shuts everything
// down gracefully and closes the store.
func (a *App) Serve(ctx context.Context) error {
	for _, e := range a.endpoints {
		if e.ln == nil {
			return errors.New("app: Serve called before Listen")
		}
	}
	errc := make(chan error, len(a.endpoints))
	for _, e := range a.endpoints {
		if e.name == "http" {
			a.log.Info("http listening", "addr", e.ln.Addr().String(), "ui", "http://"+displayAddr(e.ln.Addr()))
		} else {
			a.log.Info(e.name+" listening", "addr", e.ln.Addr().String())
		}
		go func() {
			if err := e.serve(e.ln); err != nil {
				errc <- fmt.Errorf("%s server: %w", e.name, err)
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}

	a.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	a.api.Close()
	for _, s := range []struct {
		name     string
		shutdown func(context.Context) error
	}{
		{"http", a.httpServer.Shutdown},
		{"smtp", a.smtp.Shutdown},
		{"pop3", a.pop3.Shutdown},
		{"imap", a.imap.Shutdown},
	} {
		if err := s.shutdown(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("%s shutdown: %w", s.name, err))
		}
	}
	if err := a.store.Close(); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close store: %w", err))
	}
	return runErr
}

// Run is Listen followed by Serve.
func (a *App) Run(ctx context.Context) error {
	if err := a.Listen(); err != nil {
		a.store.Close()
		return err
	}
	return a.Serve(ctx)
}

// displayAddr turns a wildcard bind address into something a browser opens.
func displayAddr(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp.IP.IsUnspecified() {
		_, port, _ := net.SplitHostPort(addr.String())
		return "localhost:" + port
	}
	return addr.String()
}

func basicAuth(creds *config.BasicAuth) func(http.Handler) http.Handler {
	user, pass := []byte(creds.User), []byte(creds.Password)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			// Evaluate both comparisons so timing does not reveal which failed.
			userOK := subtle.ConstantTimeCompare([]byte(u), user) == 1
			passOK := subtle.ConstantTimeCompare([]byte(p), pass) == 1
			if !ok || !userOK || !passOK {
				w.Header().Set("WWW-Authenticate", `Basic realm="Envry", charset="UTF-8"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !log.Enabled(r.Context(), slog.LevelDebug) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Debug("http request",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "dur", time.Since(start).Round(time.Microsecond))
		})
	}
}
