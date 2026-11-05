package smtpd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
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
	t.Cleanup(func() { st.Close() })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, opts)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		srv.Shutdown(context.Background())
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return st, ln.Addr().String()
}

var withAttachment = "From: App <app@example.test>\r\n" +
	"To: Dev <dev@test.local>\r\n" +
	"Cc: qa@test.local\r\n" +
	"Subject: Report ready\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
	"--b\r\nContent-Type: text/plain\r\n\r\nYour report is attached.\r\n.leading dot line\r\n" +
	"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=report.pdf\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4")) + "\r\n--b--\r\n"

func TestCapturesMailWithBccAndAttachment(t *testing.T) {
	st, addr := start(t, Options{MaxMessageBytes: 1 << 20})

	err := smtp.SendMail(addr, nil, "app@example.test",
		[]string{"dev@test.local", "qa@test.local", "audit@test.local"}, []byte(withAttachment))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	page, err := st.ListMessages(ctx, store.ListOptions{})
	if err != nil || page.Total != 1 {
		t.Fatalf("messages = %+v, %v", page, err)
	}
	d, err := st.GetMessage(ctx, page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "Report ready" || d.EnvelopeFrom != "app@example.test" || *d.ClientIP != "127.0.0.1" {
		t.Errorf("detail = %+v", d.MessageSummary)
	}
	if d.Text == nil || !strings.Contains(*d.Text, "\r\n.leading dot line") {
		t.Errorf("dot-stuffing not undone: %q", *d.Text)
	}
	if len(d.Attachments) != 1 || d.Attachments[0].Filename != "report.pdf" {
		t.Errorf("attachments = %+v", d.Attachments)
	}
	types := map[string]string{}
	for _, r := range d.Recipients {
		types[r.Address] = r.Type
	}
	if types["dev@test.local"] != "to" || types["qa@test.local"] != "cc" || types["audit@test.local"] != "bcc" {
		t.Errorf("recipient types = %v", types)
	}

	audit, _ := st.MailboxMessages(ctx, "audit@test.local", store.FolderInbox, store.ListOptions{})
	if len(audit.Items) != 1 || audit.Items[0].RcptType != store.RcptBcc {
		t.Errorf("bcc mailbox = %+v", audit.Items)
	}
}

func TestAnyCredentialsAcceptedByDefault(t *testing.T) {
	st, addr := start(t, Options{})
	auth := smtp.PlainAuth("", "whoever", "whatever", "127.0.0.1")
	if err := smtp.SendMail(addr, auth, "a@x.test", []string{"dev@test.local"}, []byte("Subject: hi\r\n\r\nbody\r\n")); err != nil {
		t.Fatal(err)
	}
	page, _ := st.ListMessages(context.Background(), store.ListOptions{})
	if page.Total != 1 {
		t.Errorf("total = %d", page.Total)
	}
}

func TestAuthLogin(t *testing.T) {
	_, addr := start(t, Options{})
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Auth(loginAuth{"user", "secret"}); err != nil {
		t.Fatalf("AUTH LOGIN: %v", err)
	}
}

func TestStrictAuth(t *testing.T) {
	st, addr := start(t, Options{StrictAuth: true})
	ctx := context.Background()
	if _, _, err := st.EnsureAccount(ctx, store.AccountInput{Address: "me@test.local", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	msg := []byte("From: me@test.local\r\nTo: friend@test.local\r\nSubject: hi\r\n\r\nbody\r\n")

	err := smtp.SendMail(addr, nil, "me@test.local", []string{"friend@test.local"}, msg)
	if code := smtpCode(err); code != 530 {
		t.Errorf("unauthenticated send: %v (code %d), want 530", err, code)
	}
	err = smtp.SendMail(addr, smtp.PlainAuth("", "me@test.local", "wrong", "127.0.0.1"), "me@test.local", []string{"friend@test.local"}, msg)
	if code := smtpCode(err); code != 535 {
		t.Errorf("wrong password: %v (code %d), want 535", err, code)
	}
	err = smtp.SendMail(addr, smtp.PlainAuth("", "me@test.local", "pw", "127.0.0.1"), "me@test.local", []string{"friend@test.local"}, msg)
	if err != nil {
		t.Fatalf("valid credentials: %v", err)
	}

	sent, _ := st.MailboxMessages(ctx, "me@test.local", store.FolderSent, store.ListOptions{})
	if len(sent.Items) != 1 || sent.Items[0].RcptType != store.RcptSelf {
		t.Errorf("Sent copy for authenticated account = %+v", sent.Items)
	}
}

func TestRejectsOversizedMessages(t *testing.T) {
	_, addr := start(t, Options{MaxMessageBytes: 100})
	body := "Subject: big\r\n\r\n" + strings.Repeat("x", 500) + "\r\n"
	err := smtp.SendMail(addr, nil, "a@x.test", []string{"dev@test.local"}, []byte(body))
	if code := smtpCode(err); code != 552 {
		t.Errorf("oversized: %v (code %d), want 552", err, code)
	}
}

func smtpCode(err error) int {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return tp.Code
	}
	return 0
}

// loginAuth is an AUTH LOGIN client; net/smtp only ships PLAIN and CRAM-MD5.
type loginAuth struct{ user, pass string }

func (a loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }

func (a loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch string(fromServer) {
	case "Username:":
		return []byte(a.user), nil
	case "Password:":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected challenge %q", fromServer)
}

func TestRejectRecipients(t *testing.T) {
	st, addr := start(t, Options{RejectRecipients: []string{"bounce@test.local", "*@blocked.test"}})
	msg := []byte("Subject: hi\r\n\r\nbody\r\n")

	for _, rcpt := range []string{"Bounce@Test.local", "anyone@blocked.test"} {
		err := smtp.SendMail(addr, nil, "app@example.test", []string{rcpt}, msg)
		if code := smtpCode(err); code != 550 {
			t.Errorf("%s: %v (code %d), want 550", rcpt, err, code)
		}
	}
	if err := smtp.SendMail(addr, nil, "app@example.test", []string{"dev@test.local"}, msg); err != nil {
		t.Errorf("unaffected recipient: %v", err)
	}
	page, _ := st.ListMessages(context.Background(), store.ListOptions{})
	if page.Total != 1 {
		t.Errorf("stored %d messages, want 1", page.Total)
	}
}

func TestRejectRate(t *testing.T) {
	rolls := []int{10, 90} // first below the rate, second above
	st, addr := start(t, Options{RejectRate: 50, Roll: func() int {
		r := rolls[0]
		rolls = rolls[1:]
		return r
	}})
	msg := []byte("Subject: hi\r\n\r\nbody\r\n")

	err := smtp.SendMail(addr, nil, "app@example.test", []string{"dev@test.local"}, msg)
	if code := smtpCode(err); code != 451 {
		t.Errorf("first message: %v (code %d), want 451", err, code)
	}
	if err := smtp.SendMail(addr, nil, "app@example.test", []string{"dev@test.local"}, msg); err != nil {
		t.Errorf("second message: %v", err)
	}
	page, _ := st.ListMessages(context.Background(), store.ListOptions{})
	if page.Total != 1 {
		t.Errorf("stored %d messages, want 1", page.Total)
	}
}

func TestLatency(t *testing.T) {
	_, addr := start(t, Options{Latency: 300 * time.Millisecond})
	start := time.Now()
	if err := smtp.SendMail(addr, nil, "app@example.test", []string{"dev@test.local"}, []byte("Subject: hi\r\n\r\nx\r\n")); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Errorf("sent in %v, want at least 300ms", d)
	}
}
