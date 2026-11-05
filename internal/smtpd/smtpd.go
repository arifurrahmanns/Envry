// Package smtpd is the SMTP capture server. It accepts every message, stores
// it and never relays anything.
package smtpd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"path"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/arifurrahmanns/envry/internal/store"
)

// Options configures the server.
type Options struct {
	// Domain is the hostname announced in the greeting.
	Domain string
	// MaxMessageBytes is advertised with SIZE and enforced during DATA.
	MaxMessageBytes int64
	// StrictAuth requires AUTH with an account's password before MAIL.
	StrictAuth bool
	// TLSConfig enables STARTTLS. Implicit TLS is served by passing a TLS
	// listener to Serve.
	TLSConfig *tls.Config
	Logger    *slog.Logger

	// Failure simulation, for testing how the sending app handles errors.
	// RejectRecipients are lower-cased address patterns ("*" wildcards)
	// refused at RCPT TO with 550.
	RejectRecipients []string
	// RejectRate is the percentage (0-100) of messages refused with 451
	// once DATA is complete.
	RejectRate int
	// Latency delays the reply to DATA.
	Latency time.Duration
	// Roll returns a number in [0, 100) for RejectRate; tests replace it.
	Roll func() int
}

var (
	errRecipientRejected = &smtp.SMTPError{
		Code:         550,
		EnhancedCode: smtp.EnhancedCode{5, 1, 1},
		Message:      "Recipient rejected (Envry failure simulation)",
	}
	errSimulatedFailure = &smtp.SMTPError{
		Code:         451,
		EnhancedCode: smtp.EnhancedCode{4, 3, 0},
		Message:      "Temporary failure, try again later (Envry failure simulation)",
	}
)

// Server wraps a go-smtp server backed by the store.
type Server struct {
	smtp  *smtp.Server
	store *store.Store
	log   *slog.Logger
	opts  Options
}

// New builds a server; call Serve to start accepting connections.
func New(st *store.Store, opts Options) *Server {
	if opts.Domain == "" {
		opts.Domain = "envry"
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Roll == nil {
		opts.Roll = func() int { return rand.IntN(100) }
	}
	s := &Server{store: st, log: opts.Logger, opts: opts}

	srv := smtp.NewServer(&backend{s})
	srv.Domain = opts.Domain
	srv.TLSConfig = opts.TLSConfig
	srv.MaxMessageBytes = opts.MaxMessageBytes
	srv.MaxRecipients = 1000
	srv.AllowInsecureAuth = true // local test server: AUTH without TLS is expected
	srv.EnableSMTPUTF8 = true
	srv.ReadTimeout = 5 * time.Minute
	srv.WriteTimeout = time.Minute
	srv.ErrorLog = logAdapter{opts.Logger}
	s.smtp = srv
	return s
}

// Serve accepts connections on ln until Shutdown. It may be called for
// several listeners, e.g. a plain one and a TLS one.
func (s *Server) Serve(ln net.Listener) error {
	err := s.smtp.Serve(ln)
	if errors.Is(err, smtp.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops accepting connections and waits for sessions to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.smtp.Shutdown(ctx)
}

// errAuthRequired is RFC 4954's 530 reply (go-smtp's own uses 502).
var errAuthRequired = &smtp.SMTPError{
	Code:         530,
	EnhancedCode: smtp.EnhancedCode{5, 7, 0},
	Message:      "Authentication required",
}

type backend struct{ s *Server }

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	ip := ""
	if addr, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		ip = addr.IP.String()
	}
	return &session{s: b.s, clientIP: ip}, nil
}

type session struct {
	s        *Server
	clientIP string
	authUser string
	from     string
	rcpts    []string
}

var _ smtp.AuthSession = (*session)(nil)

func (ss *session) AuthMechanisms() []string {
	return []string{sasl.Plain, sasl.Login}
}

func (ss *session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(_, username, password string) error {
			return ss.login(username, password)
		}), nil
	case sasl.Login:
		return &loginServer{auth: ss.login}, nil
	}
	return nil, smtp.ErrAuthUnknownMechanism
}

// login accepts any credentials unless strict auth is on, in which case the
// username must be an account and the password must match.
func (ss *session) login(username, password string) error {
	if ss.s.opts.StrictAuth {
		ok, err := ss.s.store.CheckPassword(context.Background(), username, password)
		if err != nil {
			ss.s.log.Error("smtp auth lookup failed", "err", err)
			return &smtp.SMTPError{Code: 454, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "Temporary authentication failure"}
		}
		if !ok {
			return smtp.ErrAuthFailed
		}
	}
	ss.authUser = username
	return nil
}

func (ss *session) Mail(from string, _ *smtp.MailOptions) error {
	if ss.s.opts.StrictAuth && ss.authUser == "" {
		return errAuthRequired
	}
	ss.from = from
	return nil
}

func (ss *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	addr := store.NormalizeAddress(to)
	for _, pattern := range ss.s.opts.RejectRecipients {
		if ok, _ := path.Match(pattern, addr); ok {
			ss.s.log.Debug("recipient rejected by failure simulation", "rcpt", addr, "pattern", pattern)
			return errRecipientRejected
		}
	}
	ss.rcpts = append(ss.rcpts, to)
	return nil
}

func (ss *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err // ErrDataTooLarge is already an SMTP 552
	}
	if d := ss.s.opts.Latency; d > 0 {
		time.Sleep(d)
	}
	if rate := ss.s.opts.RejectRate; rate > 0 && ss.s.opts.Roll() < rate {
		ss.s.log.Debug("message rejected by failure simulation", "from", ss.from, "rate", rate)
		return errSimulatedFailure
	}
	sum, err := ss.s.store.Ingest(context.Background(), store.IngestInput{
		EnvelopeFrom: ss.from,
		Recipients:   ss.rcpts,
		Raw:          raw,
		Source:       store.SourceSMTP,
		ClientIP:     ss.clientIP,
		AuthUser:     ss.authUser,
	})
	if err != nil {
		ss.s.log.Error("store message failed", "err", err)
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Could not store message"}
	}
	ss.s.log.Debug("message captured", "id", sum.ID, "from", ss.from, "rcpts", len(ss.rcpts), "bytes", len(raw))
	return nil
}

func (ss *session) Reset() {
	ss.from = ""
	ss.rcpts = nil
}

func (ss *session) Logout() error { return nil }

// loginServer implements the obsolete but widely used AUTH LOGIN mechanism,
// which go-sasl only provides as a client.
type loginServer struct {
	auth     func(username, password string) error
	step     int
	username string
}

func (l *loginServer) Next(response []byte) (challenge []byte, done bool, err error) {
	switch l.step {
	case 0:
		l.step = 1
		if len(response) == 0 {
			return []byte("Username:"), false, nil
		}
		fallthrough // the client sent the username as an initial response
	case 1:
		l.username = string(response)
		l.step = 2
		return []byte("Password:"), false, nil
	case 2:
		l.step = 3
		return nil, true, l.auth(l.username, string(response))
	}
	return nil, true, fmt.Errorf("unexpected AUTH LOGIN response")
}

type logAdapter struct{ log *slog.Logger }

func (l logAdapter) Printf(format string, v ...any) { l.log.Warn(fmt.Sprintf(format, v...)) }
func (l logAdapter) Println(v ...any)               { l.log.Warn(fmt.Sprint(v...)) }
