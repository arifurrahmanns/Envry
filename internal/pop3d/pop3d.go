// Package pop3d is a POP3 server (RFC 1939, with CAPA and response codes
// from RFC 2449) that serves each mailbox's INBOX from the store.
//
// The username is the mailbox address. Any password is accepted unless
// strict auth is on. Messages marked with DELE are removed from the
// mailbox only when the client ends the session with QUIT.
package pop3d

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arifurrahmanns/envry/internal/store"
)

const (
	// RFC 1939 requires an inactivity timer of at least 10 minutes.
	defaultIdleTimeout = 10 * time.Minute
	// RFC 2449 limits commands to 255 octets; leave headroom for clients
	// that send long usernames.
	maxLineLength = 1024
)

// Options configures the server.
type Options struct {
	// StrictAuth requires an account's password (USER/PASS or APOP).
	StrictAuth bool
	// TLSConfig enables STLS (RFC 2595). Implicit TLS (POP3S) is served by
	// passing a TLS listener to Serve.
	TLSConfig *tls.Config
	// Hostname appears in the greeting's APOP timestamp.
	Hostname    string
	IdleTimeout time.Duration
	Logger      *slog.Logger
}

// Server accepts POP3 connections.
type Server struct {
	store *store.Store
	opts  Options
	log   *slog.Logger

	mu        sync.Mutex
	listeners map[net.Listener]struct{}
	conns     map[net.Conn]struct{}
	locked    map[string]bool // mailboxes with an active session
	closed    bool
	sessions  sync.WaitGroup
}

// New builds a server; call Serve to accept connections.
func New(st *store.Store, opts Options) *Server {
	if opts.Hostname == "" {
		opts.Hostname = "envry"
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		store:     st,
		opts:      opts,
		log:       opts.Logger,
		listeners: map[net.Listener]struct{}{},
		conns:     map[net.Conn]struct{}{},
		locked:    map[string]bool{},
	}
}

// Serve accepts connections on ln until Shutdown is called. It may be
// called for several listeners, e.g. a plain one and a TLS one.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.listeners[ln] = struct{}{}
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return fmt.Errorf("pop3 accept: %w", err)
		}
		if !s.track(conn) {
			conn.Close()
			return nil
		}
		s.sessions.Add(1)
		go func() {
			defer s.sessions.Done()
			defer s.untrack(conn)
			newSession(s, conn).serve()
		}()
	}
}

// Shutdown stops accepting connections and closes open sessions. Sessions
// cut off this way never reach the UPDATE state, so nothing is deleted.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	var err error
	for ln := range s.listeners {
		if cerr := ln.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.sessions.Wait()
		close(done)
	}()
	select {
	case <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	c.Close()
}

// lock gives a session exclusive access to a maildrop (RFC 1939 section 8).
func (s *Server) lock(address string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked[address] {
		return false
	}
	s.locked[address] = true
	return true
}

func (s *Server) unlock(address string) {
	s.mu.Lock()
	delete(s.locked, address)
	s.mu.Unlock()
}

type state int

const (
	stateAuthorization state = iota
	stateTransaction
)

type message struct {
	store.MaildropMessage
	deleted bool
}

type session struct {
	s      *Server
	conn   net.Conn
	r      *bufio.Reader
	w      *bufio.Writer
	banner string // APOP timestamp, including angle brackets

	state    state
	tls      bool   // encrypted (POP3S, or after STLS)
	user     string // from USER, awaiting PASS
	mailbox  string // locked maildrop once authenticated
	messages []message
}

func newSession(s *Server, conn net.Conn) *session {
	_, isTLS := conn.(*tls.Conn)
	return &session{
		s:      s,
		conn:   conn,
		r:      bufio.NewReaderSize(conn, 4096),
		w:      bufio.NewWriterSize(conn, 32*1024),
		tls:    isTLS,
		banner: fmt.Sprintf("<%d.%d@%s>", os.Getpid(), time.Now().UnixNano(), s.opts.Hostname),
	}
}

// release gives up the session's maildrop lock, if it holds one.
func (ss *session) release() {
	if ss.mailbox != "" {
		ss.s.unlock(ss.mailbox)
		ss.mailbox = ""
	}
}

func (ss *session) serve() {
	defer ss.release()
	ss.ok("Envry POP3 server ready " + ss.banner)
	for {
		if err := ss.w.Flush(); err != nil {
			return
		}
		ss.conn.SetReadDeadline(time.Now().Add(ss.s.opts.IdleTimeout))
		line, err := ss.readLine()
		if errors.Is(err, errLineTooLong) {
			ss.err("line too long")
			continue
		}
		if err != nil {
			return // connection closed or timed out: no UPDATE state
		}
		cmd, arg, _ := strings.Cut(line, " ")
		if quit := ss.handle(strings.ToUpper(cmd), strings.TrimSpace(arg)); quit {
			ss.w.Flush()
			return
		}
	}
}

var errLineTooLong = errors.New("line too long")

func (ss *session) readLine() (string, error) {
	var line []byte
	for {
		chunk, isPrefix, err := ss.r.ReadLine()
		if err != nil {
			return "", err
		}
		line = append(line, chunk...)
		if len(line) > maxLineLength {
			for isPrefix { // discard the rest of the line
				if _, isPrefix, err = ss.r.ReadLine(); err != nil {
					return "", err
				}
			}
			return "", errLineTooLong
		}
		if !isPrefix {
			return string(line), nil
		}
	}
}

func (ss *session) ok(msg string)  { fmt.Fprintf(ss.w, "+OK %s\r\n", msg) }
func (ss *session) err(msg string) { fmt.Fprintf(ss.w, "-ERR %s\r\n", msg) }

// handle runs one command and reports whether the session is over.
func (ss *session) handle(cmd, arg string) bool {
	switch cmd {
	case "CAPA":
		ss.capa()
		return false
	case "QUIT":
		ss.quit()
		return true
	case "STLS":
		return !ss.startTLS()
	case "NOOP":
		if ss.state != stateTransaction {
			ss.err("command not valid before login")
		} else {
			ss.ok("")
		}
		return false
	}

	if ss.state == stateAuthorization {
		switch cmd {
		case "USER":
			ss.cmdUser(arg)
		case "PASS":
			ss.cmdPass(arg)
		case "APOP":
			ss.cmdAPOP(arg)
		case "STAT", "LIST", "UIDL", "RETR", "TOP", "DELE", "RSET":
			ss.err("command not valid before login")
		default:
			ss.err("unknown command")
		}
		return false
	}

	switch cmd {
	case "STAT":
		n, size := ss.totals()
		ss.ok(fmt.Sprintf("%d %d", n, size))
	case "LIST":
		ss.listing(arg, func(n int, m message) string { return fmt.Sprintf("%d %d", n, m.WireSize) })
	case "UIDL":
		ss.listing(arg, func(n int, m message) string { return fmt.Sprintf("%d %s", n, m.UniqueID()) })
	case "RETR":
		ss.cmdRetr(arg)
	case "TOP":
		ss.cmdTop(arg)
	case "DELE":
		if m, n := ss.message(arg); m != nil {
			m.deleted = true
			ss.ok(fmt.Sprintf("message %d deleted", n))
		}
	case "RSET":
		for i := range ss.messages {
			ss.messages[i].deleted = false
		}
		n, size := ss.totals()
		ss.ok(fmt.Sprintf("maildrop has %d messages (%d octets)", n, size))
	case "USER", "PASS", "APOP":
		ss.err("already logged in")
	default:
		ss.err("unknown command")
	}
	return false
}

func (ss *session) capa() {
	ss.ok("Capability list follows")
	caps := []string{"USER", "TOP", "UIDL", "RESP-CODES", "AUTH-RESP-CODE", "PIPELINING", "EXPIRE NEVER", "IMPLEMENTATION Envry"}
	if ss.canStartTLS() {
		caps = append(caps, "STLS")
	}
	for _, c := range caps {
		fmt.Fprintf(ss.w, "%s\r\n", c)
	}
	ss.w.WriteString(".\r\n")
}

func (ss *session) canStartTLS() bool {
	return ss.s.opts.TLSConfig != nil && !ss.tls && ss.state == stateAuthorization
}

// startTLS upgrades the connection (RFC 2595). It reports false when the
// session must end.
func (ss *session) startTLS() bool {
	if !ss.canStartTLS() {
		switch {
		case ss.tls:
			ss.err("connection is already encrypted")
		case ss.state != stateAuthorization:
			ss.err("STLS is only valid before login")
		default:
			ss.err("TLS is not enabled on this server")
		}
		return true
	}
	// Anything the client sent after STLS arrived before the handshake.
	// Processing it would let an attacker inject commands into the
	// encrypted session, so drop the connection instead.
	if ss.r.Buffered() > 0 {
		ss.err("unexpected data after STLS")
		return false
	}
	ss.ok("Begin TLS negotiation")
	if err := ss.w.Flush(); err != nil {
		return false
	}
	conn := tls.Server(ss.conn, ss.s.opts.TLSConfig)
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := conn.Handshake(); err != nil {
		ss.s.log.Debug("pop3 tls handshake failed", "err", err)
		return false
	}
	conn.SetDeadline(time.Time{})
	ss.conn = conn
	ss.r = bufio.NewReaderSize(conn, 4096)
	ss.w = bufio.NewWriterSize(conn, 32*1024)
	ss.tls = true
	ss.user = "" // forget anything sent before encryption
	return true
}

func (ss *session) cmdUser(arg string) {
	if arg == "" {
		ss.err("USER requires a mailbox address")
		return
	}
	ss.user = arg
	ss.ok("send PASS")
}

func (ss *session) cmdPass(arg string) {
	if ss.user == "" {
		ss.err("send USER first")
		return
	}
	user := ss.user
	ss.user = ""
	if ss.s.opts.StrictAuth {
		ok, err := ss.s.store.CheckPassword(context.Background(), user, arg)
		if err != nil {
			ss.s.log.Error("pop3 auth lookup failed", "err", err)
			ss.err("[SYS/TEMP] authentication unavailable")
			return
		}
		if !ok {
			ss.err("[AUTH] invalid username or password")
			return
		}
	}
	ss.login(user)
}

// cmdAPOP handles "APOP name digest", where digest = MD5(banner + password).
// Without strict auth any digest is accepted.
func (ss *session) cmdAPOP(arg string) {
	name, digest, ok := strings.Cut(arg, " ")
	if !ok || name == "" || digest == "" {
		ss.err("APOP requires a name and a digest")
		return
	}
	if ss.s.opts.StrictAuth {
		ok, err := ss.checkAPOP(name, strings.ToLower(strings.TrimSpace(digest)))
		if err != nil {
			ss.s.log.Error("pop3 auth lookup failed", "err", err)
			ss.err("[SYS/TEMP] authentication unavailable")
			return
		}
		if !ok {
			ss.err("[AUTH] invalid username or digest")
			return
		}
	}
	ss.login(name)
}

func (ss *session) checkAPOP(name, digest string) (bool, error) {
	pw, isAccount, err := ss.s.store.AccountPassword(context.Background(), name)
	if err != nil || !isAccount {
		return false, err
	}
	if pw == "" {
		return true, nil // account without a password accepts anything
	}
	sum := md5.Sum([]byte(ss.banner + pw))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(digest)) == 1, nil
}

func (ss *session) login(user string) {
	mailbox := store.NormalizeAddress(user)
	if !ss.s.lock(mailbox) {
		ss.err("[IN-USE] maildrop is already open in another session")
		return
	}
	list, err := ss.s.store.Maildrop(context.Background(), mailbox)
	if err != nil {
		ss.s.unlock(mailbox)
		ss.s.log.Error("pop3 load maildrop failed", "mailbox", mailbox, "err", err)
		ss.err("[SYS/TEMP] could not open maildrop")
		return
	}
	ss.mailbox = mailbox
	ss.messages = make([]message, len(list))
	for i, m := range list {
		ss.messages[i] = message{MaildropMessage: m}
	}
	ss.state = stateTransaction
	n, size := ss.totals()
	ss.ok(fmt.Sprintf("%s has %d messages (%d octets)", mailbox, n, size))
}

func (ss *session) totals() (n int, size int64) {
	for _, m := range ss.messages {
		if !m.deleted {
			n++
			size += m.WireSize
		}
	}
	return n, size
}

// message resolves a message number, writing the error reply if invalid.
func (ss *session) message(arg string) (*message, int) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		ss.err("message number required")
		return nil, 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil || n < 1 || n > len(ss.messages) {
		ss.err("no such message")
		return nil, 0
	}
	m := &ss.messages[n-1]
	if m.deleted {
		ss.err(fmt.Sprintf("message %d already deleted", n))
		return nil, 0
	}
	return m, n
}

// listing implements LIST and UIDL, with or without a message number.
func (ss *session) listing(arg string, line func(n int, m message) string) {
	if arg != "" {
		if m, n := ss.message(arg); m != nil {
			ss.ok(line(n, *m))
		}
		return
	}
	n, size := ss.totals()
	ss.ok(fmt.Sprintf("%d messages (%d octets)", n, size))
	for i, m := range ss.messages {
		if !m.deleted {
			fmt.Fprintf(ss.w, "%s\r\n", line(i+1, m))
		}
	}
	ss.w.WriteString(".\r\n")
}

func (ss *session) raw(m *message) ([]byte, bool) {
	raw, err := ss.s.store.Raw(context.Background(), m.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		ss.err("message is no longer available")
		return nil, false
	}
	if err != nil {
		ss.s.log.Error("pop3 read message failed", "id", m.MessageID, "err", err)
		ss.err("[SYS/TEMP] could not read message")
		return nil, false
	}
	return raw, true
}

func (ss *session) cmdRetr(arg string) {
	m, _ := ss.message(arg)
	if m == nil {
		return
	}
	raw, ok := ss.raw(m)
	if !ok {
		return
	}
	data := crlf(raw)
	ss.ok(fmt.Sprintf("%d octets", len(data)))
	writeDotStuffed(ss.w, data, -1)

	// Downloading counts as reading: the web UI shows it as read.
	seen := true
	if _, err := ss.s.store.UpdateDelivery(context.Background(), m.DeliveryID, store.DeliveryPatch{Seen: &seen}); err != nil &&
		!errors.Is(err, store.ErrNotFound) {
		ss.s.log.Warn("pop3 mark seen failed", "err", err)
	}
}

func (ss *session) cmdTop(arg string) {
	fields := strings.Fields(arg)
	if len(fields) != 2 {
		ss.err("usage: TOP msg lines")
		return
	}
	lines, err := strconv.Atoi(fields[1])
	if err != nil || lines < 0 {
		ss.err("line count must be a non-negative number")
		return
	}
	m, _ := ss.message(fields[0])
	if m == nil {
		return
	}
	raw, ok := ss.raw(m)
	if !ok {
		return
	}
	ss.ok("top of message follows")
	writeDotStuffed(ss.w, crlf(raw), lines)
}

// quit ends the session; in the TRANSACTION state it first removes the
// messages marked with DELE (the UPDATE state). The maildrop is unlocked
// before the reply is sent, so a client that reconnects as soon as it reads
// it finds the maildrop free.
func (ss *session) quit() {
	if ss.state != stateTransaction {
		ss.ok("Envry POP3 server signing off")
		return
	}
	var ids []int64
	for _, m := range ss.messages {
		if m.deleted {
			ids = append(ids, m.DeliveryID)
		}
	}
	mailbox := ss.mailbox
	n, err := ss.s.store.DeleteDeliveries(context.Background(), ids)
	ss.release()
	if err != nil {
		ss.s.log.Error("pop3 delete failed", "mailbox", mailbox, "err", err)
		ss.err("some deleted messages not removed")
		return
	}
	ss.ok(fmt.Sprintf("Envry POP3 server signing off (%d messages deleted)", n))
}
