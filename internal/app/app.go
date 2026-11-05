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
	"github.com/arifurrahmanns/envry/internal/webui"
)

const shutdownTimeout = 5 * time.Second

// App is one running Envry instance.
type App struct {
	cfg *config.Config
	log *slog.Logger

	httpServer *http.Server
	httpLn     net.Listener
}

// Addrs are the actual bound addresses (useful when a port was 0).
type Addrs struct {
	HTTP string
}

// New builds an App from a resolved config. It does not open any sockets.
func New(cfg *config.Config, log *slog.Logger) *App {
	a := &App{cfg: cfg, log: log}
	a.httpServer = &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return a
}

func (a *App) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(a.log))
	if a.cfg.UIAuth != nil {
		r.Use(basicAuth(a.cfg.UIAuth))
	}
	r.Mount("/api", api.New(a.cfg, a.log).Routes())

	ui := webui.New()
	if !ui.Built() {
		a.log.Warn("web UI not embedded in this build; serving a placeholder page (run `make build`)")
	}
	r.Handle("/*", ui)
	return r
}

// Listen binds every listener. Call it before Serve, or use Run.
func (a *App) Listen() error {
	ln, err := net.Listen("tcp", a.cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen http %s: %w", a.cfg.HTTPAddr, err)
	}
	a.httpLn = ln
	return nil
}

// Addrs returns the bound addresses. Valid after Listen.
func (a *App) Addrs() Addrs {
	var out Addrs
	if a.httpLn != nil {
		out.HTTP = a.httpLn.Addr().String()
	}
	return out
}

// Serve runs until ctx is cancelled or a server fails, then shuts down
// gracefully.
func (a *App) Serve(ctx context.Context) error {
	if a.httpLn == nil {
		return errors.New("app: Serve called before Listen")
	}
	a.log.Info("http listening", "addr", a.httpLn.Addr().String(), "ui", "http://"+displayAddr(a.httpLn.Addr()))

	errc := make(chan error, 1)
	go func() {
		if err := a.httpServer.Serve(a.httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
		}
		close(errc)
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errc:
		runErr = err
	}

	a.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("http shutdown: %w", err))
	}
	return runErr
}

// Run is Listen followed by Serve.
func (a *App) Run(ctx context.Context) error {
	if err := a.Listen(); err != nil {
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
