// Package app wires the store, event bus and protocol servers together and
// runs them as one process. Integration tests use it to start a full server
// on random ports (pass "127.0.0.1:0" addresses and read Addrs()).
package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arifurrahmanns/envry/internal/api"
	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/pop3d"
	"github.com/arifurrahmanns/envry/internal/smtpd"
	"github.com/arifurrahmanns/envry/internal/store"
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

	httpServer *http.Server
	httpLn     net.Listener
	smtpLn     net.Listener
	pop3Ln     net.Listener
}

// Addrs are the actual bound addresses (useful when a port was 0).
type Addrs struct {
	HTTP string
	SMTP string
	POP3 string
}

// New opens the store and builds the servers. It does not open sockets.
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

	a := &App{
		cfg:   cfg,
		log:   log,
		store: st,
		api:   api.New(cfg, st, log),
		smtp: smtpd.New(st, smtpd.Options{
			MaxMessageBytes: cfg.MaxSize,
			StrictAuth:      cfg.StrictAuth,
			Logger:          log,
		}),
		pop3: pop3d.New(st, pop3d.Options{
			StrictAuth: cfg.StrictAuth,
			Logger:     log,
		}),
	}
	a.httpServer = &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return a, nil
}

// Store exposes the store, mainly for tests.
func (a *App) Store() *store.Store { return a.store }

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
	var bound []net.Listener
	listen := func(name, addr string) (net.Listener, error) {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			for _, l := range bound {
				l.Close()
			}
			return nil, fmt.Errorf("listen %s %s: %w", name, addr, err)
		}
		bound = append(bound, ln)
		return ln, nil
	}
	var err error
	if a.smtpLn, err = listen("smtp", a.cfg.SMTPAddr); err != nil {
		return err
	}
	if a.pop3Ln, err = listen("pop3", a.cfg.POP3Addr); err != nil {
		return err
	}
	if a.httpLn, err = listen("http", a.cfg.HTTPAddr); err != nil {
		return err
	}
	return nil
}

// Addrs returns the bound addresses. Valid after Listen.
func (a *App) Addrs() Addrs {
	var out Addrs
	if a.httpLn != nil {
		out.HTTP = a.httpLn.Addr().String()
	}
	if a.smtpLn != nil {
		out.SMTP = a.smtpLn.Addr().String()
	}
	if a.pop3Ln != nil {
		out.POP3 = a.pop3Ln.Addr().String()
	}
	return out
}

// Serve runs until ctx is cancelled or a server fails, then shuts everything
// down gracefully and closes the store.
func (a *App) Serve(ctx context.Context) error {
	if a.httpLn == nil || a.smtpLn == nil || a.pop3Ln == nil {
		return errors.New("app: Serve called before Listen")
	}
	a.log.Info("smtp listening", "addr", a.smtpLn.Addr().String())
	a.log.Info("pop3 listening", "addr", a.pop3Ln.Addr().String())
	a.log.Info("http listening", "addr", a.httpLn.Addr().String(), "ui", "http://"+displayAddr(a.httpLn.Addr()))

	errc := make(chan error, 3)
	go func() {
		if err := a.httpServer.Serve(a.httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
		}
	}()
	go func() {
		if err := a.smtp.Serve(a.smtpLn); err != nil {
			errc <- fmt.Errorf("smtp server: %w", err)
		}
	}()
	go func() {
		if err := a.pop3.Serve(a.pop3Ln); err != nil {
			errc <- fmt.Errorf("pop3 server: %w", err)
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}

	a.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	a.api.Close()
	if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("http shutdown: %w", err))
	}
	if err := a.smtp.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("smtp shutdown: %w", err))
	}
	if err := a.pop3.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("pop3 shutdown: %w", err))
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
