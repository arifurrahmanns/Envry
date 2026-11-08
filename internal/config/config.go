// Package config turns command-line flags and ENVRY_* environment variables
// into a validated Config. Flags win over env; env wins over defaults.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	SMTPAddr string
	POP3Addr string
	IMAPAddr string
	HTTPAddr string

	// Implicit-TLS listeners, used only when TLS is on.
	SMTPSAddr string
	POP3SAddr string
	IMAPSAddr string

	// DBPath is a filesystem path or ":memory:".
	DBPath string

	MaxMessages int
	MaxSize     int64 // bytes

	StrictAuth bool
	Accounts   []Account

	// Failure simulation for testing how the sending app handles errors.
	// SMTPRejectRcpt holds lower-cased address patterns ("*" wildcards)
	// refused at RCPT TO with 550; SMTPRejectRate is the percentage of
	// messages refused at the end of DATA with 451; SMTPLatency delays the
	// reply to DATA.
	SMTPRejectRcpt []string
	SMTPRejectRate int
	SMTPLatency    time.Duration

	TLS     bool
	TLSCert string
	TLSKey  string

	// UIAuth enables HTTP basic auth on the web UI and API when non-nil.
	UIAuth *BasicAuth

	LogLevel slog.Level

	// Managed is set when a supervising program, such as the desktop app,
	// runs Envry: startup status goes to stdout as JSON lines and the server
	// stops when stdin is closed.
	Managed bool
}

// Account is an Inbox account declared on the command line.
// An empty Password means any password is accepted (unless strict auth).
type Account struct {
	Address  string
	Password string
}

// BasicAuth holds credentials for --ui-auth.
type BasicAuth struct {
	User     string
	Password string
}

// Flags holds the raw flag values before env overlay and validation.
type Flags struct {
	smtp, pop3, imap, http string
	smtps, pop3s, imaps    string
	db                     string
	maxMessages            int
	maxSize                string
	strictAuth             bool
	accounts               []string
	rejectRcpt             []string
	rejectRate             int
	latency                time.Duration
	tls                    bool
	tlsCert, tlsKey        string
	uiAuth                 string
	logLevel               string
	managed                bool
}

type flagSpec struct {
	name string
	env  string
	// list flags take a comma-separated env value and Set each item.
	list bool
}

var specs = []flagSpec{
	{name: "smtp", env: "ENVRY_SMTP"},
	{name: "pop3", env: "ENVRY_POP3"},
	{name: "imap", env: "ENVRY_IMAP"},
	{name: "http", env: "ENVRY_HTTP"},
	{name: "smtps", env: "ENVRY_SMTPS"},
	{name: "pop3s", env: "ENVRY_POP3S"},
	{name: "imaps", env: "ENVRY_IMAPS"},
	{name: "db", env: "ENVRY_DB"},
	{name: "max-messages", env: "ENVRY_MAX_MESSAGES"},
	{name: "max-size", env: "ENVRY_MAX_SIZE"},
	{name: "strict-auth", env: "ENVRY_STRICT_AUTH"},
	{name: "smtp-reject-rcpt", env: "ENVRY_SMTP_REJECT_RCPT", list: true},
	{name: "smtp-reject-rate", env: "ENVRY_SMTP_REJECT_RATE"},
	{name: "smtp-latency", env: "ENVRY_SMTP_LATENCY"},
	{name: "account", env: "ENVRY_ACCOUNTS", list: true},
	{name: "tls", env: "ENVRY_TLS"},
	{name: "tls-cert", env: "ENVRY_TLS_CERT"},
	{name: "tls-key", env: "ENVRY_TLS_KEY"},
	{name: "ui-auth", env: "ENVRY_UI_AUTH"},
	{name: "log-level", env: "ENVRY_LOG_LEVEL"},
	{name: "managed", env: "ENVRY_MANAGED"},
}

// DefaultDBPath is shown in help text; it is expanded at load time.
const DefaultDBPath = "~/.envry/envry.db"

// Register adds every server flag to fs and returns the struct they bind to.
func Register(fs *pflag.FlagSet) *Flags {
	f := &Flags{}
	fs.StringVar(&f.smtp, "smtp", "0.0.0.0:1025", "SMTP listen address (env ENVRY_SMTP)")
	fs.StringVar(&f.pop3, "pop3", "0.0.0.0:1110", "POP3 listen address (env ENVRY_POP3)")
	fs.StringVar(&f.imap, "imap", "0.0.0.0:1143", "IMAP listen address (env ENVRY_IMAP)")
	fs.StringVar(&f.http, "http", "0.0.0.0:8025", "HTTP (web UI + API) listen address (env ENVRY_HTTP)")
	fs.StringVar(&f.smtps, "smtps", "0.0.0.0:1465", "SMTP over implicit TLS listen address, with --tls (env ENVRY_SMTPS)")
	fs.StringVar(&f.pop3s, "pop3s", "0.0.0.0:1995", "POP3 over implicit TLS listen address, with --tls (env ENVRY_POP3S)")
	fs.StringVar(&f.imaps, "imaps", "0.0.0.0:1993", "IMAP over implicit TLS listen address, with --tls (env ENVRY_IMAPS)")
	fs.StringVar(&f.db, "db", DefaultDBPath, `SQLite database path, or ":memory:" (env ENVRY_DB)`)
	fs.IntVar(&f.maxMessages, "max-messages", 1000, "keep only the newest N messages, 0 = unlimited (env ENVRY_MAX_MESSAGES)")
	fs.StringVar(&f.maxSize, "max-size", "25MB", "maximum accepted message size, e.g. 512KB, 25MB (env ENVRY_MAX_SIZE)")
	fs.BoolVar(&f.strictAuth, "strict-auth", false, "require per-account passwords on SMTP/POP3/IMAP (env ENVRY_STRICT_AUTH)")
	fs.StringArrayVar(&f.rejectRcpt, "smtp-reject-rcpt", nil, "refuse these recipients with 550, * wildcards allowed, repeatable (env ENVRY_SMTP_REJECT_RCPT, comma-separated)")
	fs.IntVar(&f.rejectRate, "smtp-reject-rate", 0, "refuse this percentage of messages with 451, 0-100 (env ENVRY_SMTP_REJECT_RATE)")
	fs.DurationVar(&f.latency, "smtp-latency", 0, "delay every SMTP DATA reply, e.g. 2s (env ENVRY_SMTP_LATENCY)")
	fs.StringArrayVar(&f.accounts, "account", nil, "Inbox account as address[:password], repeatable (env ENVRY_ACCOUNTS, comma-separated)")
	fs.BoolVar(&f.tls, "tls", false, "enable STARTTLS and the implicit-TLS ports (env ENVRY_TLS)")
	fs.StringVar(&f.tlsCert, "tls-cert", "", "TLS certificate file; self-signed if empty (env ENVRY_TLS_CERT)")
	fs.StringVar(&f.tlsKey, "tls-key", "", "TLS private key file; self-signed if empty (env ENVRY_TLS_KEY)")
	fs.StringVar(&f.uiAuth, "ui-auth", "", "protect the web UI and API with basic auth, user:pass (env ENVRY_UI_AUTH)")
	fs.StringVar(&f.logLevel, "log-level", "info", "log level: debug, info, warn, error (env ENVRY_LOG_LEVEL)")
	fs.BoolVar(&f.managed, "managed", false, "run under a supervising app: report startup as JSON lines on stdout, stop when stdin closes (env ENVRY_MANAGED)")
	// Only the desktop app needs this; keep it out of --help.
	_ = fs.MarkHidden("managed")
	return f
}

// Load overlays environment variables onto flags the user did not set
// explicitly, then validates and converts everything into a Config.
// lookupEnv is normally os.LookupEnv; tests pass a fake.
func (f *Flags) Load(fs *pflag.FlagSet, lookupEnv func(string) (string, bool)) (*Config, error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	for _, s := range specs {
		fl := fs.Lookup(s.name)
		if fl == nil || fl.Changed {
			continue
		}
		v, ok := lookupEnv(s.env)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		items := []string{v}
		if s.list {
			items = splitList(v)
		}
		for _, item := range items {
			if err := fs.Set(s.name, item); err != nil {
				return nil, fmt.Errorf("%s=%q: %w", s.env, v, err)
			}
		}
	}
	return f.build()
}

func (f *Flags) build() (*Config, error) {
	var errs []error
	c := &Config{
		MaxMessages: f.maxMessages,
		StrictAuth:  f.strictAuth,
		TLS:         f.tls,
		TLSCert:     f.tlsCert,
		TLSKey:      f.tlsKey,
		Managed:     f.managed,
	}

	for _, a := range []struct {
		name string
		val  string
		dst  *string
	}{
		{"smtp", f.smtp, &c.SMTPAddr},
		{"pop3", f.pop3, &c.POP3Addr},
		{"imap", f.imap, &c.IMAPAddr},
		{"http", f.http, &c.HTTPAddr},
		{"smtps", f.smtps, &c.SMTPSAddr},
		{"pop3s", f.pop3s, &c.POP3SAddr},
		{"imaps", f.imaps, &c.IMAPSAddr},
	} {
		if _, _, err := net.SplitHostPort(a.val); err != nil {
			errs = append(errs, fmt.Errorf("--%s %q: want host:port: %w", a.name, a.val, err))
		}
		*a.dst = a.val
	}

	db, err := expandHome(f.db)
	if err != nil {
		errs = append(errs, fmt.Errorf("--db: %w", err))
	}
	c.DBPath = db

	if c.MaxMessages < 0 {
		errs = append(errs, fmt.Errorf("--max-messages must be >= 0, got %d", c.MaxMessages))
	}

	size, err := ParseSize(f.maxSize)
	if err != nil {
		errs = append(errs, fmt.Errorf("--max-size: %w", err))
	}
	c.MaxSize = size

	for _, raw := range f.accounts {
		acct, err := parseAccount(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("--account %q: %w", raw, err))
			continue
		}
		c.Accounts = append(c.Accounts, acct)
	}

	for _, raw := range f.rejectRcpt {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
			errs = append(errs, fmt.Errorf("--smtp-reject-rcpt %q: invalid pattern", raw))
			continue
		}
		c.SMTPRejectRcpt = append(c.SMTPRejectRcpt, pattern)
	}
	if f.rejectRate < 0 || f.rejectRate > 100 {
		errs = append(errs, fmt.Errorf("--smtp-reject-rate must be between 0 and 100, got %d", f.rejectRate))
	}
	c.SMTPRejectRate = f.rejectRate
	if f.latency < 0 {
		errs = append(errs, errors.New("--smtp-latency must not be negative"))
	}
	c.SMTPLatency = f.latency

	if (c.TLSCert == "") != (c.TLSKey == "") {
		errs = append(errs, errors.New("--tls-cert and --tls-key must be set together"))
	}
	if c.TLSCert != "" {
		c.TLS = true
	}

	if f.uiAuth != "" {
		user, pass, ok := strings.Cut(f.uiAuth, ":")
		if !ok || user == "" || pass == "" {
			errs = append(errs, errors.New("--ui-auth: want user:pass"))
		} else {
			c.UIAuth = &BasicAuth{User: user, Password: pass}
		}
	}

	if err := c.LogLevel.UnmarshalText([]byte(f.logLevel)); err != nil {
		errs = append(errs, fmt.Errorf("--log-level: %w", err))
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseAccount accepts "address" or "address:password". The password may
// itself contain colons; only the first colon separates it.
func parseAccount(raw string) (Account, error) {
	addr, pass, _ := strings.Cut(strings.TrimSpace(raw), ":")
	addr = strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 || strings.ContainsAny(addr, " \t<>") {
		return Account{}, errors.New("want an email address like dev@test.local")
	}
	return Account{Address: addr, Password: pass}, nil
}

func expandHome(p string) (string, error) {
	if p == ":memory:" || !(p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`)) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}

// ParseSize parses a byte size such as "25MB", "512KB", "1GiB" or "1048576".
// Units are binary (1KB = 1024 bytes), which matches how mail clients
// usually report sizes.
func ParseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	if t == "" {
		return 0, errors.New("empty size")
	}
	i := len(t)
	for i > 0 && (t[i-1] < '0' || t[i-1] > '9') {
		i--
	}
	num, unit := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i:])
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	var mult int64
	switch unit {
	case "", "B":
		mult = 1
	case "K", "KB", "KIB":
		mult = 1 << 10
	case "M", "MB", "MIB":
		mult = 1 << 20
	case "G", "GB", "GIB":
		mult = 1 << 30
	default:
		return 0, fmt.Errorf("invalid size unit %q in %q", unit, s)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q too large", s)
	}
	return n * mult, nil
}
