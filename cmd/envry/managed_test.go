package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// managedArgs runs the server on free loopback ports with an in-memory store.
func managedArgs(extra ...string) []string {
	return append([]string{
		"serve", "--managed", "--log-level", "error", "--db", ":memory:",
		"--smtp", "127.0.0.1:0", "--pop3", "127.0.0.1:0", "--imap", "127.0.0.1:0", "--http", "127.0.0.1:0",
	}, extra...)
}

func TestManagedReadyThenStopOnStdinClose(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	cmd := newRootCmd()
	cmd.SetIn(stdinR)
	cmd.SetOut(stdoutW)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(managedArgs())

	done := make(chan error, 1)
	go func() {
		err := cmd.Execute()
		stdoutW.Close()
		done <- err
	}()

	line, err := bufio.NewReader(stdoutR).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdoutR) }()

	var s status
	if err := json.Unmarshal([]byte(line), &s); err != nil {
		t.Fatalf("status %q is not JSON: %v", line, err)
	}
	if s.Event != "ready" || s.Addrs == nil {
		t.Fatalf("status = %+v, want a ready event with addresses", s)
	}
	for name, addr := range map[string]string{"http": s.Addrs.HTTP, "smtp": s.Addrs.SMTP, "pop3": s.Addrs.POP3, "imap": s.Addrs.IMAP} {
		if _, port, err := net.SplitHostPort(addr); err != nil || port == "0" {
			t.Errorf("%s address %q is not a bound host:port", name, addr)
		}
	}

	// The HTTP server answers until stdin closes.
	c, err := net.Dial("tcp", s.Addrs.HTTP)
	if err != nil {
		t.Fatalf("dial HTTP: %v", err)
	}
	c.Close()

	stdinW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v after stdin closed, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after stdin closed")
	}
}

func TestManagedReportsPortInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	// Keep stdin open: closing it would stop startup before the port check.
	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetIn(stdinR)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(managedArgs("--imap", busy.Addr().String()))
	if err := cmd.Execute(); err == nil {
		t.Fatal("serve succeeded on a busy port")
	}

	var s status
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatalf("output %q is not one JSON line: %v", out.String(), err)
	}
	if s.Event != "error" || s.Service != "imap" || s.Addr != busy.Addr().String() || !s.InUse {
		t.Errorf("status = %+v, want an in-use error for imap on %s", s, busy.Addr())
	}
	if !strings.Contains(s.Hint, "already in use") {
		t.Errorf("hint %q does not explain the conflict", s.Hint)
	}
}

func TestManagedFromFlagOrEnv(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	env := func(k string) (string, bool) { return "1", k == "ENVRY_MANAGED" }

	root := newRootCmd()
	if managed(root, noEnv) {
		t.Error("managed without flag or env")
	}
	if !managed(root, env) {
		t.Error("ENVRY_MANAGED=1 not seen")
	}
	if err := root.PersistentFlags().Set("managed", "true"); err != nil {
		t.Fatal(err)
	}
	if !managed(root, noEnv) {
		t.Error("--managed not seen")
	}
}
