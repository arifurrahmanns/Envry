package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/arifurrahmanns/envry/internal/app"
	"github.com/arifurrahmanns/envry/internal/version"
)

// Managed mode (--managed) is how the desktop app runs Envry. Envry writes
// one JSON line to stdout when its sockets are open ("ready") or when it
// cannot start or stops with an error ("error"), and it shuts down cleanly
// when stdin is closed, which also happens if the desktop app crashes.

// status is one line of managed-mode output.
type status struct {
	Event   string      `json:"event"` // "ready" or "error"
	Version string      `json:"version,omitempty"`
	Addrs   *statusAddr `json:"addrs,omitempty"`

	// Error details. Service and Addr are set when a socket could not be
	// opened; InUse when that is because another program holds the port.
	Message string `json:"message,omitempty"`
	Service string `json:"service,omitempty"`
	Addr    string `json:"addr,omitempty"`
	InUse   bool   `json:"inUse,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// statusAddr lists the bound addresses; TLS ones are empty when TLS is off.
type statusAddr struct {
	HTTP  string `json:"http"`
	SMTP  string `json:"smtp"`
	POP3  string `json:"pop3"`
	IMAP  string `json:"imap"`
	SMTPS string `json:"smtps,omitempty"`
	POP3S string `json:"pop3s,omitempty"`
	IMAPS string `json:"imaps,omitempty"`
}

func readyStatus(a app.Addrs) status {
	return status{
		Event:   "ready",
		Version: version.Version,
		Addrs: &statusAddr{
			HTTP: a.HTTP, SMTP: a.SMTP, POP3: a.POP3, IMAP: a.IMAP,
			SMTPS: a.SMTPS, POP3S: a.POP3S, IMAPS: a.IMAPS,
		},
	}
}

func errorStatus(err error) status {
	s := status{Event: "error", Message: err.Error(), Hint: hint(err)}
	var le *app.ListenError
	if errors.As(err, &le) {
		s.Service = le.Service
		s.Addr = le.Addr
		s.InUse = addrInUse(le.Err)
	}
	return s
}

// writeStatus writes s as a single JSON line.
func writeStatus(w io.Writer, s status) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// managed reports whether managed mode was asked for, by flag or by
// ENVRY_MANAGED, for code that runs after the command (and its parsed
// config) is gone.
func managed(root *cobra.Command, lookupEnv func(string) (string, bool)) bool {
	if f := root.PersistentFlags().Lookup("managed"); f != nil && f.Changed {
		return f.Value.String() == "true"
	}
	v, _ := lookupEnv("ENVRY_MANAGED")
	on, _ := strconv.ParseBool(v)
	return on
}

// stopOnEOF returns a context that is cancelled once r reaches EOF or fails,
// i.e. when the supervising app closes Envry's stdin or goes away.
func stopOnEOF(ctx context.Context, r io.Reader) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		_, _ = io.Copy(io.Discard, r)
		cancel()
	}()
	return ctx, cancel
}
