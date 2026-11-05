package imapd

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/arifurrahmanns/envry/internal/store"
)

type env struct {
	t     *testing.T
	store *store.Store
	addr  string
}

func start(t *testing.T, opts Options) *env {
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
	return &env{t: t, store: st, addr: ln.Addr().String()}
}

func (e *env) deliver(to, subject, body string, extraRcpts ...string) {
	e.t.Helper()
	raw := fmt.Sprintf("From: Ava <ava@northwind.test>\r\nTo: %s\r\nSubject: %s\r\nDate: Thu, 05 Nov 2026 09:00:00 +0000\r\n"+
		"Message-ID: <%d@test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", to, subject, time.Now().UnixNano(), body)
	if _, err := e.store.Ingest(context.Background(), store.IngestInput{
		EnvelopeFrom: "ava@northwind.test", Recipients: append([]string{to}, extraRcpts...), Raw: []byte(raw),
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) dial(user string, handler *imapclient.UnilateralDataHandler) *imapclient.Client {
	e.t.Helper()
	c, err := imapclient.DialInsecure(e.addr, &imapclient.Options{UnilateralDataHandler: handler})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { c.Close() })
	if user != "" {
		if err := c.Login(user, "any password").Wait(); err != nil {
			e.t.Fatalf("login %s: %v", user, err)
		}
	}
	return c
}

func (e *env) inbox(address string) []store.FolderMessage {
	e.t.Helper()
	f, err := e.store.MailFolder(context.Background(), address, "INBOX")
	if err != nil {
		e.t.Fatal(err)
	}
	msgs, err := e.store.FolderMessages(context.Background(), f.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return msgs
}

func TestListAndSelect(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "Welcome", "Hello")
	e.deliver("dev@test.local", "Second", "Again")
	c := e.dial("Dev@Test.local", nil)

	if !c.Caps().Has(imap.CapIMAP4rev1) || !c.Caps().Has(imap.CapMove) || !c.Caps().Has(imap.CapUIDPlus) {
		t.Errorf("caps = %v", c.Caps())
	}

	list, err := c.List("", "*", nil).Collect()
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string][]imap.MailboxAttr{}
	for _, l := range list {
		attrs[l.Mailbox] = l.Attrs
	}
	if len(attrs) != 4 || !slices.Contains(attrs["Sent"], imap.MailboxAttrSent) ||
		!slices.Contains(attrs["Trash"], imap.MailboxAttrTrash) || !slices.Contains(attrs["Drafts"], imap.MailboxAttrDrafts) {
		t.Errorf("LIST = %v", attrs)
	}

	data, err := c.Select("inbox", nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if data.NumMessages != 2 || data.UIDValidity == 0 || data.UIDNext != 3 {
		t.Errorf("SELECT = %+v", data)
	}

	st, err := c.Status("INBOX", &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true, Size: true}).Wait()
	if err != nil || *st.NumMessages != 2 || *st.NumUnseen != 2 || st.UIDNext != 3 || *st.Size <= 0 {
		t.Errorf("STATUS = %+v, %v", st, err)
	}
	if _, err := c.Select("Nope", nil).Wait(); err == nil {
		t.Error("SELECT of a missing mailbox succeeded")
	}
}

func TestFetch(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "Invoice INV-2041", "Total due: $49.00\r\n.dot line")
	c := e.dial("dev@test.local", nil)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}

	peek := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		UID: true, Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{}, BodySection: []*imap.FetchItemBodySection{peek},
	}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("FETCH = %v, %v", msgs, err)
	}
	m := msgs[0]
	body := m.FindBodySection(peek)
	if m.UID != 1 || m.Envelope.Subject != "Invoice INV-2041" || m.Envelope.From[0].Addr() != "ava@northwind.test" {
		t.Errorf("envelope = %+v", m.Envelope)
	}
	if int64(len(body)) != m.RFC822Size || !strings.Contains(string(body), "\r\n.dot line\r\n") {
		t.Errorf("RFC822.SIZE %d, body %d bytes: %q", m.RFC822Size, len(body), body)
	}
	if bs, ok := m.BodyStructure.(*imap.BodyStructureSinglePart); !ok || bs.Type != "text" || bs.Subtype != "plain" {
		t.Errorf("BODYSTRUCTURE = %#v", m.BodyStructure)
	}
	if len(m.Flags) != 0 {
		t.Errorf("BODY.PEEK[] set flags: %v", m.Flags)
	}

	// BODY[] without PEEK marks the message \Seen, in the store too.
	full := &imap.FetchItemBodySection{}
	msgs, err = c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{full}}).Collect()
	if err != nil || !slices.Contains(msgs[0].Flags, imap.FlagSeen) {
		t.Errorf("FETCH BODY[] flags = %v, %v", msgs[0].Flags, err)
	}
	if got := e.inbox("dev@test.local")[0].Flags; !slices.Contains(got, store.FlagSeen) {
		t.Errorf("store flags after BODY[] = %v", got)
	}

	head := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	msgs, _ = c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{head}}).Collect()
	if h := string(msgs[0].FindBodySection(head)); !strings.HasPrefix(h, "From: Ava") || !strings.HasSuffix(h, "\r\n\r\n") {
		t.Errorf("BODY[HEADER] = %q", h)
	}
}

func TestStoreAndSearch(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "Reset your password", "Use this verification link")
	e.deliver("dev@test.local", "Weekly digest", "Nothing new")
	e.deliver("dev@test.local", "Invoice INV-2041", "Total due")
	c := e.dial("dev@test.local", nil)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}

	res, err := c.Store(imap.SeqSetNum(2), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen, imap.FlagFlagged, "$Important"}}, nil).Collect()
	if err != nil || len(res) != 1 || len(res[0].Flags) != 3 {
		t.Fatalf("STORE = %+v, %v", res, err)
	}
	stored := e.inbox("dev@test.local")[1].Flags
	if !slices.Contains(stored, store.FlagSeen) || !slices.Contains(stored, store.FlagFlagged) || !slices.Contains(stored, "$Important") {
		t.Errorf("stored flags = %v", stored)
	}
	page, _ := e.store.MailboxMessages(context.Background(), "dev@test.local", "INBOX", store.ListOptions{})
	for _, d := range page.Items {
		if d.Subject == "Weekly digest" && (!d.Seen || !d.Flagged) {
			t.Errorf("web view of the digest = seen %v flagged %v", d.Seen, d.Flagged)
		}
	}

	cases := []struct {
		name string
		crit *imap.SearchCriteria
		want []uint32
	}{
		{"all", &imap.SearchCriteria{}, []uint32{1, 2, 3}},
		{"unseen", &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, []uint32{1, 3}},
		{"flagged", &imap.SearchCriteria{Flag: []imap.Flag{imap.FlagFlagged}}, []uint32{2}},
		{"keyword", &imap.SearchCriteria{Flag: []imap.Flag{"$important"}}, []uint32{2}},
		{"subject", &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: "invoice"}}}, []uint32{3}},
		{"from", &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "From", Value: "NORTHWIND"}}}, []uint32{1, 2, 3}},
		{"body partial word", &imap.SearchCriteria{Body: []string{"verif"}}, []uint32{1}},
		{"text matches header", &imap.SearchCriteria{Text: []string{"digest"}}, []uint32{2}},
		{"not", &imap.SearchCriteria{Not: []imap.SearchCriteria{{Body: []string{"total"}}}}, []uint32{1, 2}},
		{"or", &imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{{Body: []string{"verification"}}, {Body: []string{"total"}}}}}, []uint32{1, 3}},
		{"uid range", &imap.SearchCriteria{UID: []imap.UIDSet{imap.UIDSetNum(2, 3)}}, []uint32{2, 3}},
		{"larger", &imap.SearchCriteria{Larger: 1}, []uint32{1, 2, 3}},
		{"sent since", &imap.SearchCriteria{SentSince: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}, nil},
	}
	for _, tc := range cases {
		data, err := c.Search(tc.crit, nil).Wait()
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := data.AllSeqNums(); !slices.Equal(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
			t.Errorf("%s: SEARCH = %v, want %v", tc.name, got, tc.want)
		}
	}
	uids, err := c.UIDSearch(&imap.SearchCriteria{Body: []string{"total"}}, nil).Wait()
	if err != nil || !slices.Equal(uids.AllUIDs(), []imap.UID{3}) {
		t.Errorf("UID SEARCH = %v, %v", uids, err)
	}
}

func TestCopyMoveExpungeAppend(t *testing.T) {
	e := start(t, Options{})
	for i := 1; i <= 4; i++ {
		e.deliver("dev@test.local", fmt.Sprint("msg ", i), "body")
	}
	c := e.dial("dev@test.local", nil)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}

	cp, err := c.Copy(imap.SeqSetNum(1, 2), "Trash").Wait()
	if err != nil || cp.UIDValidity == 0 || cp.DestUIDs.String() != "1:2" || cp.SourceUIDs.String() != "1:2" {
		t.Fatalf("COPY = %+v, %v", cp, err)
	}
	if _, err := c.Copy(imap.SeqSetNum(1), "Missing").Wait(); err == nil || !strings.Contains(err.Error(), "TRYCREATE") {
		t.Errorf("COPY to missing folder: %v", err)
	}

	if err := c.Create("Archive/2026", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	mv, err := c.Move(imap.SeqSetNum(3), "Archive/2026").Wait()
	if err != nil || mv.DestUIDs.String() != "1" {
		t.Fatalf("MOVE = %+v, %v", mv, err)
	}
	if n := c.Mailbox().NumMessages; n != 3 {
		t.Errorf("after MOVE the client sees %d messages, want 3", n)
	}

	if err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	expunged, err := c.Expunge().Collect()
	if err != nil || !slices.Equal(expunged, []uint32{1}) {
		t.Fatalf("EXPUNGE = %v, %v", expunged, err)
	}
	if got := len(e.inbox("dev@test.local")); got != 2 {
		t.Errorf("INBOX has %d messages in the store, want 2", got)
	}
	page, _ := e.store.ListMessages(context.Background(), store.ListOptions{})
	if page.Total != 4 {
		t.Errorf("EXPUNGE removed messages from Sent: %d left", page.Total)
	}

	raw := "From: dev@test.local\r\nTo: friend@test.local\r\nSubject: Saved draft\r\n\r\nDraft body\r\n"
	app := c.Append("Drafts", int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}})
	app.Write([]byte(raw))
	app.Close()
	ad, err := app.Wait()
	if err != nil || ad.UID != 1 || ad.UIDValidity == 0 {
		t.Fatalf("APPEND = %+v, %v", ad, err)
	}
	drafts, _ := e.store.MailboxMessages(context.Background(), "dev@test.local", "Drafts", store.ListOptions{})
	if len(drafts.Items) != 1 || drafts.Items[0].Subject != "Saved draft" || drafts.Items[0].Source != store.SourceIMAPAppend {
		t.Errorf("Drafts = %+v", drafts.Items)
	}

	if err := c.Rename("Archive", "Old", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	st, err := c.Status("Old/2026", &imap.StatusOptions{NumMessages: true}).Wait()
	if err != nil || *st.NumMessages != 1 {
		t.Errorf("renamed child folder STATUS = %+v, %v", st, err)
	}
	if err := c.Delete("INBOX").Wait(); err == nil {
		t.Error("deleting INBOX succeeded")
	}
	if err := c.Delete("Old/2026").Wait(); err != nil {
		t.Errorf("DELETE: %v", err)
	}
	if err := c.Create("Trash", nil).Wait(); err == nil || !strings.Contains(err.Error(), "ALREADYEXISTS") {
		t.Errorf("CREATE existing: %v", err)
	}
}

func TestIdleReceivesLiveUpdates(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "Existing", "x")

	exists := make(chan uint32, 8)
	flags := make(chan []imap.Flag, 8)
	expunges := make(chan uint32, 8)
	c := e.dial("dev@test.local", &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				exists <- *d.NumMessages
			}
		},
		Fetch: func(m *imapclient.FetchMessageData) {
			buf, _ := m.Collect()
			if buf != nil {
				flags <- buf.Flags
			}
		},
		Expunge: func(seq uint32) { expunges <- seq },
	})
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	idle, err := c.Idle()
	if err != nil {
		t.Fatal(err)
	}

	// New mail over SMTP (here: straight into the store) → * 2 EXISTS.
	start := time.Now()
	e.deliver("dev@test.local", "Arrived while idling", "y")
	select {
	case n := <-exists:
		if n != 2 {
			t.Errorf("EXISTS %d, want 2", n)
		}
		t.Logf("EXISTS pushed in %v", time.Since(start))
	case <-time.After(3 * time.Second):
		t.Fatal("no EXISTS while idling")
	}

	// Marking it read in the web UI → * 1 FETCH (FLAGS (\Seen)).
	page, _ := e.store.MailboxMessages(context.Background(), "dev@test.local", "INBOX", store.ListOptions{})
	yes := true
	if _, err := e.store.UpdateDelivery(context.Background(), page.Items[1].DeliveryID, store.DeliveryPatch{Seen: &yes}); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-flags:
		if !slices.Contains(f, imap.FlagSeen) {
			t.Errorf("FETCH FLAGS = %v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no FETCH FLAGS while idling")
	}

	// Deleting it in the web UI → * 1 EXPUNGE.
	if err := e.store.DeleteDelivery(context.Background(), page.Items[1].DeliveryID); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-expunges:
		if seq != 1 {
			t.Errorf("EXPUNGE %d, want 1", seq)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no EXPUNGE while idling")
	}

	if err := idle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idle.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestTwoSessionsStayInSync(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "One", "x")
	e.deliver("dev@test.local", "Two", "x")

	expunged := make(chan uint32, 4)
	b := e.dial("dev@test.local", &imapclient.UnilateralDataHandler{Expunge: func(seq uint32) { expunged <- seq }})
	a := e.dial("dev@test.local", nil)
	for _, c := range []*imapclient.Client{a, b} {
		if _, err := c.Select("INBOX", nil).Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Expunge().Collect(); err != nil {
		t.Fatal(err)
	}
	if err := b.Noop().Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-expunged:
		if seq != 1 {
			t.Errorf("session B saw EXPUNGE %d", seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session B never saw the EXPUNGE")
	}
	if n := b.Mailbox().NumMessages; n != 1 {
		t.Errorf("session B sees %d messages", n)
	}
}

func TestExamineIsReadOnly(t *testing.T) {
	e := start(t, Options{})
	e.deliver("dev@test.local", "Look only", "x")
	c := e.dial("dev@test.local", nil)
	if _, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	full := &imap.FetchItemBodySection{}
	if _, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{full}}).Collect(); err != nil {
		t.Fatal(err)
	}
	if f := e.inbox("dev@test.local")[0].Flags; len(f) != 0 {
		t.Errorf("EXAMINE + BODY[] changed flags: %v", f)
	}
	if err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagFlagged}}, nil).Close(); err == nil {
		t.Error("STORE succeeded in a read-only mailbox")
	}
}

func TestLogin(t *testing.T) {
	e := start(t, Options{StrictAuth: true})
	e.store.EnsureAccount(context.Background(), store.AccountInput{Address: "dev@test.local", Password: "s3cret"})

	c := e.dial("", nil)
	if err := c.Login("dev@test.local", "wrong").Wait(); err == nil {
		t.Error("wrong password accepted")
	}
	if err := c.Login("stranger@test.local", "s3cret").Wait(); err == nil {
		t.Error("non-account login accepted")
	}
	if err := c.Login("dev@test.local", "s3cret").Wait(); err != nil {
		t.Errorf("valid login: %v", err)
	}

	open := start(t, Options{})
	c2 := open.dial("", nil)
	if err := c2.Login("brand-new@test.local", "anything").Wait(); err != nil {
		t.Fatalf("permissive login: %v", err)
	}
	if list, err := c2.List("", "*", nil).Collect(); err != nil || len(list) != 4 {
		t.Errorf("new mailbox folders = %d, %v", len(list), err)
	}
}
