package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func load(t *testing.T, args []string, env map[string]string) (*Config, error) {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	f := Register(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f.Load(fs, func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
}

func TestDefaults(t *testing.T) {
	c, err := load(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPAddr != "0.0.0.0:1025" || c.POP3Addr != "0.0.0.0:1110" ||
		c.IMAPAddr != "0.0.0.0:1143" || c.HTTPAddr != "0.0.0.0:8025" {
		t.Errorf("unexpected default addrs: %+v", c)
	}
	if c.MaxMessages != 1000 {
		t.Errorf("MaxMessages = %d", c.MaxMessages)
	}
	if c.MaxSize != 25<<20 {
		t.Errorf("MaxSize = %d", c.MaxSize)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, ".envry", "envry.db"); c.DBPath != want {
		t.Errorf("DBPath = %q, want %q", c.DBPath, want)
	}
	if c.LogLevel != slog.LevelInfo || c.StrictAuth || c.TLS || c.UIAuth != nil || len(c.Accounts) != 0 {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestEnvOverlaysDefaults(t *testing.T) {
	c, err := load(t, nil, map[string]string{
		"ENVRY_SMTP":         "127.0.0.1:2525",
		"ENVRY_DB":           ":memory:",
		"ENVRY_MAX_MESSAGES": "5",
		"ENVRY_MAX_SIZE":     "512KB",
		"ENVRY_STRICT_AUTH":  "true",
		"ENVRY_ACCOUNTS":     "Dev@Test.local:secret, qa@test.local",
		"ENVRY_UI_AUTH":      "admin:pw",
		"ENVRY_LOG_LEVEL":    "debug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPAddr != "127.0.0.1:2525" || c.DBPath != ":memory:" || c.MaxMessages != 5 ||
		c.MaxSize != 512<<10 || !c.StrictAuth || c.LogLevel != slog.LevelDebug {
		t.Errorf("env not applied: %+v", c)
	}
	want := []Account{{"dev@test.local", "secret"}, {"qa@test.local", ""}}
	if len(c.Accounts) != 2 || c.Accounts[0] != want[0] || c.Accounts[1] != want[1] {
		t.Errorf("Accounts = %+v, want %+v", c.Accounts, want)
	}
	if c.UIAuth == nil || *c.UIAuth != (BasicAuth{"admin", "pw"}) {
		t.Errorf("UIAuth = %+v", c.UIAuth)
	}
}

func TestFlagsWinOverEnv(t *testing.T) {
	c, err := load(t,
		[]string{"--smtp", "127.0.0.1:3000", "--account", "a@x.test:p:w", "--max-messages", "0"},
		map[string]string{
			"ENVRY_SMTP":         "127.0.0.1:2525",
			"ENVRY_ACCOUNTS":     "b@x.test",
			"ENVRY_MAX_MESSAGES": "9",
		})
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPAddr != "127.0.0.1:3000" {
		t.Errorf("SMTPAddr = %q", c.SMTPAddr)
	}
	if c.MaxMessages != 0 {
		t.Errorf("MaxMessages = %d", c.MaxMessages)
	}
	if len(c.Accounts) != 1 || c.Accounts[0] != (Account{"a@x.test", "p:w"}) {
		t.Errorf("Accounts = %+v", c.Accounts)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string][]string{
		"bad addr":     {"--http", "8025"},
		"bad size":     {"--max-size", "10XB"},
		"bad account":  {"--account", "not-an-email"},
		"half tls":     {"--tls-cert", "cert.pem"},
		"bad ui-auth":  {"--ui-auth", "nopass"},
		"bad loglevel": {"--log-level", "loud"},
		"negative max": {"--max-messages", "-1"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := load(t, args, nil); err == nil {
				t.Errorf("expected error for %v", args)
			}
		})
	}
}

func TestBadEnvValue(t *testing.T) {
	if _, err := load(t, nil, map[string]string{"ENVRY_STRICT_AUTH": "maybe"}); err == nil {
		t.Error("expected error for non-bool ENVRY_STRICT_AUTH")
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"0": 0, "1024": 1024, "10B": 10, "1k": 1024, "512KB": 512 << 10,
		"25MB": 25 << 20, "25 mb": 25 << 20, "2MiB": 2 << 20, "1GB": 1 << 30,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "MB", "-1MB", "1.5MB", "12TB"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) should fail", bad)
		}
	}
}
