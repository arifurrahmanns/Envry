package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"

	"github.com/arifurrahmanns/envry/internal/app"
)

// hint explains errors the user can fix themselves; it returns "" otherwise.
func hint(err error) string {
	var le *app.ListenError
	if !errors.As(err, &le) || !addrInUse(le.Err) {
		return ""
	}
	_, port, _ := net.SplitHostPort(le.Addr)
	name := strings.ToUpper(le.Service)
	if le.Service == "http" {
		name = "web UI"
	}
	return fmt.Sprintf("Port %s (%s) is already in use. Another mail catcher such as Mailpit or MailHog, "+
		"or a second Envry, may be running. Stop it, or choose another port, e.g. envry --%s :%s",
		port, name, le.Service, otherPort(le.Service))
}

// otherPort suggests a port, near the usual alternatives, for a service.
func otherPort(service string) string {
	switch service {
	case "smtp":
		return "2525"
	case "pop3":
		return "2110"
	case "imap":
		return "2143"
	case "smtps":
		return "2465"
	case "pop3s":
		return "2995"
	case "imaps":
		return "2993"
	}
	return "8026"
}

// addrInUse reports a bind failure because the address is taken.
func addrInUse(err error) bool {
	const wsaeaddrinuse = syscall.Errno(10048) // Windows' code for the same error
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, wsaeaddrinuse)
}
