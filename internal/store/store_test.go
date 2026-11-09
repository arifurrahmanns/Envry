package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/events"
)

type fixture struct {
	*Store
	events <-chan events.Event
}

func newStore(t *testing.T, maxMessages int) fixture {
	t.Helper()
	bus := events.New()
	ch, cancel := bus.Subscribe(256)
	t.Cleanup(cancel)
	clock := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	s, err := Open(context.Background(), Options{
		Path:        ":memory:",
		MaxMessages: maxMessages,
		Bus:         bus,
		Now: func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return fixture{s, ch}
}

func (f fixture) drain() []events.Event {
	var out []events.Event
	for {
		select {
		case e := <-f.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

type mail struct {
	from, to, cc, subject, body string
	attachment                  bool
}

func (m mail) raw() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\n", m.from, m.to)
	if m.cc != "" {
		fmt.Fprintf(&b, "Cc: %s\r\n", m.cc)
	}
	fmt.Fprintf(&b, "Subject: %s\r\nDate: Thu, 05 Nov 2026 09:00:00 +0000\r\nMessage-ID: <%s@test>\r\nMIME-Version: 1.0\r\n",
		m.subject, strings.ReplaceAll(m.subject, " ", "-"))
	if !m.attachment {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", m.body)
		return []byte(b.String())
	}
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\n%s\r\n"+
		"--b\r\nContent-Type: text/csv\r\nContent-Disposition: attachment; filename=report.csv\r\n\r\na,b\r\n1,2\r\n--b--\r\n", m.body)
	return []byte(b.String())
}

func (f fixture) ingest(t *testing.T, m mail, rcpts ...string) *MessageSummary {
	t.Helper()
	sum, err := f.Ingest(context.Background(), IngestInput{
		EnvelopeFrom: "app@example.test", Recipients: rcpts, Raw: m.raw(), ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

var ctx = context.Background()

func TestIngestDeliversWithRecipientTypes(t *testing.T) {
	f := newStore(t, 0)
	sum := f.ingest(t, mail{
		from: "App <app@example.test>", to: "Dev User <Dev@Test.local>", cc: "qa@test.local",
		subject: "Welcome", body: "Hello there", attachment: true,
	}, "dev@test.local", "QA@test.local", "boss@test.local", "dev@test.local")

	if sum.Subject != "Welcome" || sum.Preview != "Hello there" || sum.AttachmentCount != 1 || !sum.HasText {
		t.Errorf("summary = %+v", sum)
	}

	d, err := f.GetMessage(ctx, sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{{"dev@test.local", RcptTo}, {"qa@test.local", RcptCc}, {"boss@test.local", RcptBcc}}
	if fmt.Sprint(d.Recipients) != fmt.Sprint(want) {
		t.Errorf("Recipients = %v, want %v", d.Recipients, want)
	}
	if d.EnvelopeFrom != "app@example.test" || d.ClientIP == nil || *d.ClientIP != "127.0.0.1" {
		t.Errorf("envelope = %q, ip = %v", d.EnvelopeFrom, d.ClientIP)
	}
	if d.Text == nil || strings.TrimSpace(*d.Text) != "Hello there" || len(d.Headers) == 0 {
		t.Errorf("text = %v, headers = %d", d.Text, len(d.Headers))
	}
	if len(d.Attachments) != 1 || d.Attachments[0].Filename != "report.csv" || d.Attachments[0].ID != "1" {
		t.Fatalf("Attachments = %+v", d.Attachments)
	}
	a, body, err := f.AttachmentContent(ctx, sum.ID, "1")
	if err != nil || a.ContentType != "text/csv" || strings.TrimSpace(string(body)) != "a,b\r\n1,2" {
		t.Errorf("AttachmentContent = %+v, %q, %v", a, body, err)
	}
	if _, _, err := f.AttachmentContent(ctx, sum.ID, "9"); err != ErrNotFound {
		t.Errorf("missing attachment err = %v", err)
	}

	mbs, err := f.ListMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 3 {
		t.Fatalf("mailboxes = %+v", mbs)
	}
	for _, mb := range mbs {
		if len(mb.Folders) != 4 || mb.Total != 1 || mb.Unread != 1 || mb.LastReceivedAt == nil {
			t.Errorf("mailbox %s = %+v", mb.Address, mb)
		}
		if mb.Address == "dev@test.local" && (mb.DisplayName == nil || *mb.DisplayName != "Dev User") {
			t.Errorf("dev display name = %v", mb.DisplayName)
		}
	}

	page, err := f.MailboxMessages(ctx, "boss@test.local", "", ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].RcptType != RcptBcc || page.Items[0].Folder != FolderInbox {
		t.Errorf("boss inbox = %+v, %v", page, err)
	}

	var created, mailboxes int
	for _, e := range f.drain() {
		switch e.Type {
		case events.MessageCreated:
			created++
		case events.MailboxCreated:
			mailboxes++
		}
	}
	if created != 1 || mailboxes != 3 {
		t.Errorf("events: %d message.created, %d mailbox.created", created, mailboxes)
	}
}

func TestUIDsIncreasePerFolder(t *testing.T) {
	f := newStore(t, 0)
	for i := 0; i < 3; i++ {
		f.ingest(t, mail{from: "a@x.test", to: "dev@test.local", subject: fmt.Sprint("m", i)}, "dev@test.local")
	}
	var uids []int
	rows, err := f.db.Query(`SELECT d.uid FROM deliveries d JOIN folders f ON f.id = d.folder_id WHERE f.name = 'INBOX' ORDER BY d.uid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var u int
		rows.Scan(&u)
		uids = append(uids, u)
	}
	if fmt.Sprint(uids) != "[1 2 3]" {
		t.Errorf("uids = %v", uids)
	}
}

func TestRetentionPrunesOldest(t *testing.T) {
	f := newStore(t, 5)
	var ids []string
	for i := 0; i < 8; i++ {
		ids = append(ids, f.ingest(t, mail{from: "a@x.test", to: "dev@test.local", subject: fmt.Sprint("msg ", i)}, "dev@test.local").ID)
	}
	page, err := f.ListMessages(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || page.Items[0].ID != ids[7] || page.Items[4].ID != ids[3] {
		t.Errorf("after prune: total %d, first %s", page.Total, page.Items[0].ID)
	}
	if _, err := f.GetMessage(ctx, ids[0]); err != ErrNotFound {
		t.Errorf("oldest message still present: %v", err)
	}
	var fts, deliveries int
	f.db.QueryRow(`SELECT COUNT(*) FROM messages_fts`).Scan(&fts)
	f.db.QueryRow(`SELECT COUNT(*) FROM deliveries`).Scan(&deliveries)
	if fts != 5 || deliveries != 5 {
		t.Errorf("fts rows = %d, deliveries = %d, want 5 and 5", fts, deliveries)
	}
	var deleted int
	for _, e := range f.drain() {
		if e.Type == events.MessageDeleted {
			deleted++
		}
	}
	if deleted != 3 {
		t.Errorf("message.deleted events = %d, want 3", deleted)
	}
}

func TestSearchAndPagination(t *testing.T) {
	f := newStore(t, 0)
	f.ingest(t, mail{from: "billing@acme.test", to: "dev@test.local", subject: "Invoice INV-2041", body: "Total due"}, "dev@test.local")
	f.ingest(t, mail{from: "security@acme.test", to: "qa@test.local", subject: "Reset your password", body: "Use this verification link"}, "qa@test.local")
	for i := 0; i < 3; i++ {
		f.ingest(t, mail{from: "news@acme.test", to: "dev@test.local", subject: fmt.Sprint("Digest ", i), body: "weekly"}, "dev@test.local")
	}

	cases := map[string]int{"invoice": 1, "verif": 1, "acme": 5, "reset password": 1, "qa@test.local": 1, "nothing-here": 0}
	for q, want := range cases {
		page, err := f.ListMessages(ctx, ListOptions{Query: q})
		if err != nil || page.Total != want || len(page.Items) != want {
			t.Errorf("Query(%q) = total %d, items %d, err %v; want %d", q, page.Total, len(page.Items), err, want)
		}
	}
	// FTS5 syntax in user input must not cause errors.
	for _, q := range []string{`"unbalanced`, `AND OR NOT`, `a:b (c`, `*`, `-x`} {
		if _, err := f.ListMessages(ctx, ListOptions{Query: q}); err != nil {
			t.Errorf("Query(%q): %v", q, err)
		}
	}

	if page, _ := f.ListMessages(ctx, ListOptions{To: "QA@test.local"}); page.Total != 1 {
		t.Errorf("To filter total = %d", page.Total)
	}
	if page, _ := f.ListMessages(ctx, ListOptions{From: "billing@acme.test"}); page.Total != 1 {
		t.Errorf("From filter total = %d", page.Total)
	}

	var seen []string
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page, err := f.ListMessages(ctx, ListOptions{Limit: 2, Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 5 {
			t.Fatalf("Total = %d on every page", page.Total)
		}
		for _, m := range page.Items {
			seen = append(seen, m.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("paged through %d messages, want 5", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] >= seen[i-1] {
			t.Errorf("not newest first: %v", seen)
		}
	}
}

func TestDeliveryFlagsArePerMailbox(t *testing.T) {
	f := newStore(t, 0)
	sum := f.ingest(t, mail{from: "a@x.test", to: "dev@test.local, qa@test.local", subject: "Hi"}, "dev@test.local", "qa@test.local")

	dev, _ := f.MailboxMessages(ctx, "dev@test.local", FolderInbox, ListOptions{})
	yes := true
	d, err := f.UpdateDelivery(ctx, dev.Items[0].DeliveryID, DeliveryPatch{Seen: &yes})
	if err != nil || !d.Seen || d.Flagged {
		t.Fatalf("UpdateDelivery = %+v, %v", d, err)
	}
	qa, _ := f.MailboxMessages(ctx, "qa@test.local", FolderInbox, ListOptions{})
	if qa.Items[0].Seen {
		t.Error("marking dev's copy read also marked qa's copy")
	}
	if _, err := f.UpdateDelivery(ctx, 9999, DeliveryPatch{Seen: &yes}); err != ErrNotFound {
		t.Errorf("unknown delivery err = %v", err)
	}

	if err := f.DeleteDelivery(ctx, dev.Items[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	if page, _ := f.MailboxMessages(ctx, "dev@test.local", FolderInbox, ListOptions{}); page.Total != 0 {
		t.Errorf("dev inbox total = %d after delete", page.Total)
	}
	if _, err := f.GetMessage(ctx, sum.ID); err != nil {
		t.Errorf("message gone after deleting one delivery: %v", err)
	}
	if page, _ := f.MailboxMessages(ctx, "qa@test.local", FolderInbox, ListOptions{}); page.Total != 1 {
		t.Errorf("qa inbox total = %d", page.Total)
	}
	// dev@ has no mail left and is not an account, so it is not listed.
	mbs, _ := f.ListMailboxes(ctx)
	if len(mbs) != 1 || mbs[0].Address != "qa@test.local" {
		t.Errorf("mailboxes = %+v", mbs)
	}
}

func TestAccountsAndSentCopy(t *testing.T) {
	f := newStore(t, 0)
	mb, created, err := f.EnsureAccount(ctx, AccountInput{Address: "Me@Test.local", DisplayName: "Me", Password: "pw"})
	if err != nil || !created || !mb.IsAccount || mb.Address != "me@test.local" || len(mb.Folders) != 4 {
		t.Fatalf("EnsureAccount = %+v, %v, %v", mb, created, err)
	}
	if _, created, _ := f.EnsureAccount(ctx, AccountInput{Address: "me@test.local"}); created {
		t.Error("second EnsureAccount reported created")
	}

	_, err = f.Ingest(ctx, IngestInput{
		EnvelopeFrom: "me@test.local", Recipients: []string{"friend@test.local"},
		Raw: mail{from: "me@test.local", to: "friend@test.local", subject: "From me"}.raw(), AuthUser: "ME@test.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := f.MailboxMessages(ctx, "me@test.local", FolderSent, ListOptions{})
	if len(sent.Items) != 1 || sent.Items[0].RcptType != RcptSelf || !sent.Items[0].Seen {
		t.Errorf("Sent copy = %+v", sent.Items)
	}

	if ok, _ := f.CheckPassword(ctx, "me@test.local", "pw"); !ok {
		t.Error("correct password rejected")
	}
	if ok, _ := f.CheckPassword(ctx, "me@test.local", "nope"); ok {
		t.Error("wrong password accepted")
	}
	if ok, _ := f.CheckPassword(ctx, "friend@test.local", "x"); ok {
		t.Error("non-account login accepted")
	}

	if err := f.DeleteAllMessages(ctx); err != nil {
		t.Fatal(err)
	}
	mbs, _ := f.ListMailboxes(ctx)
	if len(mbs) != 1 || mbs[0].Address != "me@test.local" || mbs[0].Total != 0 {
		t.Errorf("after clear: %+v", mbs)
	}
	st, _ := f.Stats(ctx)
	if st.Messages != 0 || st.Accounts != 1 {
		t.Errorf("stats = %+v", st)
	}

	if err := f.RemoveAccount(ctx, "me@test.local"); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveAccount(ctx, "me@test.local"); err != ErrNotFound {
		t.Errorf("second RemoveAccount err = %v", err)
	}
}

func TestLatestMatching(t *testing.T) {
	f := newStore(t, 0)
	start := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	f.ingest(t, mail{from: "a@x.test", to: "dev@test.local", subject: "Reset your password"}, "dev@test.local")
	second := f.ingest(t, mail{from: "a@x.test", to: "dev@test.local", subject: "Reset your password again"}, "dev@test.local")

	m, err := f.LatestMatching(ctx, WaitFilter{To: "dev@test.local", Subject: "RESET"})
	if err != nil || m.ID != second.ID {
		t.Errorf("LatestMatching = %+v, %v", m, err)
	}
	if _, err := f.LatestMatching(ctx, WaitFilter{To: "qa@test.local"}); err != ErrNotFound {
		t.Errorf("no-match err = %v", err)
	}
	if _, err := f.LatestMatching(ctx, WaitFilter{Since: start.Add(time.Hour)}); err != ErrNotFound {
		t.Errorf("since filter err = %v", err)
	}
	if _, err := f.LatestMatching(ctx, WaitFilter{Subject: "100%_off"}); err != ErrNotFound {
		t.Errorf("LIKE wildcards not escaped: %v", err)
	}
}

func TestDeleteMessage(t *testing.T) {
	f := newStore(t, 0)
	sum := f.ingest(t, mail{from: "a@x.test", to: "dev@test.local", subject: "Bye"}, "dev@test.local")
	if err := f.DeleteMessage(ctx, sum.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteMessage(ctx, sum.ID); err != ErrNotFound {
		t.Errorf("second delete err = %v", err)
	}
	if _, err := f.Raw(ctx, sum.ID); err != ErrNotFound {
		t.Errorf("raw still present: %v", err)
	}
}

func TestFileDatabaseReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "envry.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, IngestInput{Recipients: []string{"dev@test.local"}, Raw: mail{from: "a@x.test", to: "dev@test.local", subject: "Persist"}.raw()}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	page, err := s.ListMessages(ctx, ListOptions{})
	if err != nil || page.Total != 1 || page.Items[0].Subject != "Persist" {
		t.Errorf("after reopen: %+v, %v", page, err)
	}
}

func TestProviderWebhooks(t *testing.T) {
	s := newStore(t, 0).Store
	ctx := context.Background()
	w, found, err := s.ProviderWebhook(ctx, "resend")
	if err != nil || found || w.Provider != "resend" || w.Enabled {
		t.Fatalf("before saving: %+v %v %v", w, found, err)
	}
	if _, err := s.SaveProviderWebhook(ctx, ProviderWebhook{Provider: "resend", URL: "http://localhost:3000/hooks", Secret: "whsec_x", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProviderWebhook(ctx, ProviderWebhook{Provider: "resend", URL: "http://localhost:3000/hooks/resend", Secret: "whsec_y", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	w, found, err = s.ProviderWebhook(ctx, "resend")
	if err != nil || !found || w.URL != "http://localhost:3000/hooks/resend" || w.Secret != "whsec_y" || w.Enabled || w.UpdatedAt.IsZero() {
		t.Errorf("after saving: %+v %v %v", w, found, err)
	}
}
