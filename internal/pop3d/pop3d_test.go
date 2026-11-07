package pop3d

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/store"
)

func start(t *testing.T, opts Options) (*store.Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, opts)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
		st.Close()
	})
	return st, ln.Addr().String()
}

func deliver(t *testing.T, st *store.Store, to, subject, body string) {
	t.Helper()
	raw := fmt.Sprintf("From: app@example.test\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", to, subject, body)
	if _, err := st.Ingest(context.Background(), store.IngestInput{
		EnvelopeFrom: "app@example.test", Recipients: []string{to}, Raw: []byte(raw),
	}); err != nil {
		t.Fatal(err)
	}
}

type client struct {
	t      *testing.T
	conn   net.Conn
	r      *bufio.Reader
	banner string
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &client{t: t, conn: conn, r: bufio.NewReader(conn)}
	c.banner = c.line()
	return c
}

func (c *client) line() string {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	s, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v (got %q)", err, s)
	}
	if !strings.HasSuffix(s, "\r\n") {
		c.t.Fatalf("line without CRLF: %q", s)
	}
	return strings.TrimSuffix(s, "\r\n")
}

func (c *client) send(cmd string) {
	c.t.Helper()
	if _, err := fmt.Fprintf(c.conn, "%s\r\n", cmd); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) cmd(cmd string) string {
	c.t.Helper()
	c.send(cmd)
	return c.line()
}

// multi sends a command and returns the status line plus the raw body lines
// (still dot-stuffed) up to the terminating ".".
func (c *client) multi(cmd string) (string, []string) {
	c.t.Helper()
	status := c.cmd(cmd)
	if !strings.HasPrefix(status, "+OK") {
		return status, nil
	}
	var lines []string
	for {
		l := c.line()
		if l == "." {
			return status, lines
		}
		lines = append(lines, l)
	}
}

func (c *client) login(user string) {
	c.t.Helper()
	if r := c.cmd("USER " + user); !strings.HasPrefix(r, "+OK") {
		c.t.Fatalf("USER: %s", r)
	}
	if r := c.cmd("PASS anything"); !strings.HasPrefix(r, "+OK") {
		c.t.Fatalf("PASS: %s", r)
	}
}

func TestSessionCommands(t *testing.T) {
	st, addr := start(t, Options{})
	deliver(t, st, "dev@test.local", "First", "Hello\r\n.leading dot\r\n..two dots\r\nend\r\n")
	deliver(t, st, "dev@test.local", "Second", "line 1\r\nline 2\r\nline 3\r\n")
	deliver(t, st, "qa@test.local", "Not yours", "x\r\n")

	drop, _ := st.Maildrop(context.Background(), "dev@test.local")
	size1, size2 := drop[0].WireSize, drop[1].WireSize
	total := size1 + size2

	c := dial(t, addr)
	if !regexp.MustCompile(`^\+OK Envry POP3 server ready <\d+\.\d+@envry>$`).MatchString(c.banner) {
		t.Fatalf("banner = %q", c.banner)
	}

	steps := []struct {
		cmd   string
		multi bool
		want  string   // status line prefix
		lines []string // exact multi-line body, when non-nil
	}{
		{cmd: "CAPA", multi: true, want: "+OK", lines: []string{"USER", "TOP", "UIDL", "RESP-CODES", "AUTH-RESP-CODE", "PIPELINING", "EXPIRE NEVER", "IMPLEMENTATION Envry"}},
		{cmd: "STAT", want: "-ERR command not valid before login"},
		{cmd: "NOOP", want: "-ERR"},
		{cmd: "PASS secret", want: "-ERR send USER first"},
		{cmd: "USER", want: "-ERR"},
		{cmd: "FOO", want: "-ERR unknown command"},
		{cmd: "user Dev@Test.local", want: "+OK"},
		{cmd: "pass whatever", want: fmt.Sprintf("+OK dev@test.local has 2 messages (%d octets)", total)},
		{cmd: "USER again", want: "-ERR already logged in"},
		{cmd: "STAT", want: fmt.Sprintf("+OK 2 %d", total)},
		{cmd: "LIST", multi: true, want: "+OK 2 messages", lines: []string{fmt.Sprintf("1 %d", size1), fmt.Sprintf("2 %d", size2)}},
		{cmd: "LIST 2", want: fmt.Sprintf("+OK 2 %d", size2)},
		{cmd: "LIST 3", want: "-ERR no such message"},
		{cmd: "LIST x", want: "-ERR no such message"},
		{cmd: "UIDL", multi: true, want: "+OK", lines: []string{"1 " + drop[0].UniqueID(), "2 " + drop[1].UniqueID()}},
		{cmd: "UIDL 1", want: "+OK 1 " + drop[0].UniqueID()},
		{cmd: "TOP 2 0", multi: true, want: "+OK", lines: []string{"From: app@example.test", "To: dev@test.local", "Subject: Second", ""}},
		{cmd: "TOP 2 2", multi: true, want: "+OK", lines: []string{"From: app@example.test", "To: dev@test.local", "Subject: Second", "", "line 1", "line 2"}},
		{cmd: "TOP 2", want: "-ERR usage"},
		{cmd: "TOP 2 -1", want: "-ERR"},
		{cmd: "RETR 1", multi: true, want: fmt.Sprintf("+OK %d octets", size1), lines: []string{
			"From: app@example.test", "To: dev@test.local", "Subject: First", "",
			"Hello", "..leading dot", "...two dots", "end"}},
		{cmd: "RETR", want: "-ERR message number required"},
		{cmd: "DELE 1", want: "+OK message 1 deleted"},
		{cmd: "DELE 1", want: "-ERR message 1 already deleted"},
		{cmd: "RETR 1", want: "-ERR message 1 already deleted"},
		{cmd: "STAT", want: fmt.Sprintf("+OK 1 %d", size2)},
		{cmd: "LIST", multi: true, want: "+OK 1 messages", lines: []string{fmt.Sprintf("2 %d", size2)}},
		{cmd: "RSET", want: fmt.Sprintf("+OK maildrop has 2 messages (%d octets)", total)},
		{cmd: "NOOP", want: "+OK"},
		{cmd: "QUIT", want: "+OK Envry POP3 server signing off (0 messages deleted)"},
	}
	for _, s := range steps {
		var status string
		var lines []string
		if s.multi {
			status, lines = c.multi(s.cmd)
		} else {
			status = c.cmd(s.cmd)
		}
		if !strings.HasPrefix(status, s.want) {
			t.Errorf("%s: status %q, want prefix %q", s.cmd, status, s.want)
			continue
		}
		if s.lines != nil && strings.Join(lines, "\n") != strings.Join(s.lines, "\n") {
			t.Errorf("%s: lines\n%q\nwant\n%q", s.cmd, lines, s.lines)
		}
	}
}

func TestRetrOctetsMatchList(t *testing.T) {
	st, addr := start(t, Options{})
	// Bare LF line endings must be served as CRLF and counted that way.
	if _, err := st.Ingest(context.Background(), store.IngestInput{
		Recipients: []string{"dev@test.local"},
		Raw:        []byte("Subject: lf\n\nunix\nline endings\n.dot"),
	}); err != nil {
		t.Fatal(err)
	}
	c := dial(t, addr)
	c.login("dev@test.local")
	list := c.cmd("LIST 1")
	size, _ := strconv.Atoi(strings.Fields(list)[2])

	c.send("RETR 1")
	status := c.line()
	var got int
	for {
		l := c.line()
		if l == "." {
			break
		}
		l = strings.TrimPrefix(l, ".") // undo dot-stuffing
		got += len(l) + 2
	}
	if status != fmt.Sprintf("+OK %d octets", size) || got != size {
		t.Errorf("LIST says %d, RETR status %q, received %d octets", size, status, got)
	}
}

func TestDeleteOnlyOnQuit(t *testing.T) {
	st, addr := start(t, Options{})
	for i := 1; i <= 3; i++ {
		deliver(t, st, "dev@test.local", fmt.Sprint("m", i), "x\r\n")
	}
	count := func() int {
		drop, err := st.Maildrop(context.Background(), "dev@test.local")
		if err != nil {
			t.Fatal(err)
		}
		return len(drop)
	}

	// Dropping the connection without QUIT never enters UPDATE.
	c := dial(t, addr)
	c.login("dev@test.local")
	c.cmd("DELE 1")
	c.conn.Close()
	time.Sleep(100 * time.Millisecond)
	if n := count(); n != 3 {
		t.Fatalf("after disconnect: %d messages, want 3", n)
	}

	c = dial(t, addr)
	c.login("dev@test.local")
	c.cmd("DELE 1")
	c.cmd("DELE 3")
	if n := count(); n != 3 {
		t.Fatalf("before QUIT: %d messages, want 3", n)
	}
	if r := c.cmd("QUIT"); r != "+OK Envry POP3 server signing off (2 messages deleted)" {
		t.Fatalf("QUIT = %q", r)
	}
	drop, _ := st.Maildrop(context.Background(), "dev@test.local")
	if len(drop) != 1 {
		t.Fatalf("after QUIT: %d messages, want 1", len(drop))
	}
	page, _ := st.ListMessages(context.Background(), store.ListOptions{})
	if page.Total != 3 {
		t.Errorf("POP3 delete removed the message from Sent too: %d left", page.Total)
	}
}

func TestRetrMarksSeen(t *testing.T) {
	st, addr := start(t, Options{})
	deliver(t, st, "dev@test.local", "Read me", "x\r\n")
	c := dial(t, addr)
	c.login("dev@test.local")
	c.multi("TOP 1 0")
	page, _ := st.MailboxMessages(context.Background(), "dev@test.local", store.FolderInbox, store.ListOptions{})
	if page.Items[0].Seen {
		t.Fatal("TOP marked the message read")
	}
	c.multi("RETR 1")
	page, _ = st.MailboxMessages(context.Background(), "dev@test.local", store.FolderInbox, store.ListOptions{})
	if !page.Items[0].Seen {
		t.Error("RETR did not mark the message read")
	}
}

func TestEmptyMaildrop(t *testing.T) {
	_, addr := start(t, Options{})
	c := dial(t, addr)
	if r := c.cmd("USER nobody@test.local"); !strings.HasPrefix(r, "+OK") {
		t.Fatal(r)
	}
	if r := c.cmd("PASS x"); r != "+OK nobody@test.local has 0 messages (0 octets)" {
		t.Errorf("PASS = %q", r)
	}
	if r := c.cmd("STAT"); r != "+OK 0 0" {
		t.Errorf("STAT = %q", r)
	}
	if r := c.cmd("RETR 1"); r != "-ERR no such message" {
		t.Errorf("RETR = %q", r)
	}
}

func TestMaildropLock(t *testing.T) {
	_, addr := start(t, Options{})
	a := dial(t, addr)
	a.login("dev@test.local")

	b := dial(t, addr)
	b.cmd("USER dev@test.local")
	if r := b.cmd("PASS x"); !strings.HasPrefix(r, "-ERR [IN-USE]") {
		t.Errorf("second session = %q", r)
	}
	a.cmd("QUIT")
	b.cmd("USER dev@test.local")
	if r := b.cmd("PASS x"); !strings.HasPrefix(r, "+OK") {
		t.Errorf("after first session quit = %q", r)
	}
}

// A client that reconnects as soon as it reads the QUIT reply must find the
// maildrop unlocked; polling clients do exactly this.
func TestQuitReleasesLockBeforeReply(t *testing.T) {
	_, addr := start(t, Options{})
	for i := range 200 {
		c := dial(t, addr)
		c.cmd("USER dev@test.local")
		if r := c.cmd("PASS x"); !strings.HasPrefix(r, "+OK") {
			t.Fatalf("login %d right after the previous QUIT: %q", i, r)
		}
		if r := c.cmd("QUIT"); !strings.HasPrefix(r, "+OK") {
			t.Fatalf("QUIT %d: %q", i, r)
		}
	}
}

func TestStrictAuth(t *testing.T) {
	st, addr := start(t, Options{StrictAuth: true})
	ctx := context.Background()
	st.EnsureAccount(ctx, store.AccountInput{Address: "dev@test.local", Password: "s3cret"})
	st.EnsureAccount(ctx, store.AccountInput{Address: "open@test.local"})

	cases := []struct{ user, pass, want string }{
		{"dev@test.local", "wrong", "-ERR [AUTH]"},
		{"stranger@test.local", "s3cret", "-ERR [AUTH]"},
		{"open@test.local", "anything", "+OK"},
		{"dev@test.local", "s3cret", "+OK"},
	}
	for _, tc := range cases {
		c := dial(t, addr)
		c.cmd("USER " + tc.user)
		if r := c.cmd("PASS " + tc.pass); !strings.HasPrefix(r, tc.want) {
			t.Errorf("%s/%s: %q, want %q", tc.user, tc.pass, r, tc.want)
		}
		c.cmd("QUIT")
	}

	apop := func(user, secret string) string {
		c := dial(t, addr)
		ts := c.banner[strings.Index(c.banner, "<"):]
		sum := md5.Sum([]byte(ts + secret))
		r := c.cmd("APOP " + user + " " + hex.EncodeToString(sum[:]))
		c.cmd("QUIT")
		return r
	}
	if r := apop("dev@test.local", "s3cret"); !strings.HasPrefix(r, "+OK") {
		t.Errorf("APOP with right secret = %q", r)
	}
	if r := apop("dev@test.local", "wrong"); !strings.HasPrefix(r, "-ERR [AUTH]") {
		t.Errorf("APOP with wrong secret = %q", r)
	}
}

func TestAPOPPermissive(t *testing.T) {
	_, addr := start(t, Options{})
	c := dial(t, addr)
	if r := c.cmd("APOP dev@test.local 0123456789abcdef0123456789abcdef"); !strings.HasPrefix(r, "+OK dev@test.local has") {
		t.Errorf("APOP = %q", r)
	}
	if r := c.cmd("APOP"); r != "-ERR already logged in" {
		t.Errorf("second APOP = %q", r)
	}
}

func TestLongLineRejected(t *testing.T) {
	_, addr := start(t, Options{})
	c := dial(t, addr)
	if r := c.cmd("USER " + strings.Repeat("a", 5000)); r != "-ERR line too long" {
		t.Errorf("long line = %q", r)
	}
	if r := c.cmd("CAPA"); r != "+OK Capability list follows" {
		t.Errorf("session unusable after long line: %q", r)
	}
}

func TestIdleTimeoutClosesSession(t *testing.T) {
	_, addr := start(t, Options{IdleTimeout: 200 * time.Millisecond})
	c := dial(t, addr)
	c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.r.ReadString('\n'); err == nil {
		t.Error("connection still open after the idle timeout")
	}
}
