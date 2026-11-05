package mimeparse

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func parseFile(t *testing.T, name string) (*Message, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return m, raw
}

func TestAlternative(t *testing.T) {
	m, _ := parseFile(t, "alternative.eml")

	if m.From != (Address{"Ava Lindqvist", "ava@northwind.test"}) {
		t.Errorf("From = %+v", m.From)
	}
	wantTo := []Address{{"Dev User", "dev@test.local"}, {"", "qa@test.local"}}
	if !reflect.DeepEqual(m.To, wantTo) {
		t.Errorf("To = %+v", m.To)
	}
	if len(m.Cc) != 1 || m.Cc[0].Address != "ops@test.local" || len(m.ReplyTo) != 1 {
		t.Errorf("Cc = %+v, ReplyTo = %+v", m.Cc, m.ReplyTo)
	}
	if m.Subject != "Welcome to Northwind" || m.MessageID != "welcome-1@northwind.test" {
		t.Errorf("Subject = %q, MessageID = %q", m.Subject, m.MessageID)
	}
	if !m.Date.Equal(time.Date(2026, 11, 5, 14, 34, 0, 0, time.UTC)) {
		t.Errorf("Date = %v", m.Date)
	}
	if !m.HasText || !m.HasHTML || !strings.Contains(m.Text, "Thanks for signing up.") ||
		!strings.Contains(m.HTML, "<b>signing up</b>") {
		t.Errorf("bodies: text=%q html=%q", m.Text, m.HTML)
	}
	if len(m.Attachments) != 0 {
		t.Errorf("Attachments = %+v", m.Attachments)
	}
	if got := m.Preview(); got != "Hi there, Thanks for signing up." {
		t.Errorf("Preview = %q", got)
	}
	if len(m.Headers) == 0 || m.Headers[0].Name != "From" {
		t.Errorf("Headers = %+v", m.Headers)
	}
}

func TestRelatedWithAttachment(t *testing.T) {
	m, raw := parseFile(t, "related-attachment.eml")

	if m.HasText || !m.HasHTML || !strings.Contains(m.HTML, "cid:logo@northwind") {
		t.Fatalf("bodies: text=%v html=%q", m.HasText, m.HTML)
	}
	want := []Attachment{
		{ContentType: "image/png", ContentID: "logo@northwind", Inline: true, Size: 8, PartPath: "1.2"},
		{Filename: "INV-2041.pdf", ContentType: "application/pdf", Size: 9, PartPath: "2"},
	}
	if !reflect.DeepEqual(m.Attachments, want) {
		t.Fatalf("Attachments =\n%+v\nwant\n%+v", m.Attachments, want)
	}

	typ, body, err := ExtractPart(raw, "2")
	if err != nil || typ != "application/pdf" || string(body) != "%PDF-1.4\n" {
		t.Errorf("ExtractPart(2) = %q, %q, %v", typ, body, err)
	}
	typ, body, err = ExtractPart(raw, "1.2")
	if err != nil || typ != "image/png" || len(body) != 8 {
		t.Errorf("ExtractPart(1.2) = %q, %d bytes, %v", typ, len(body), err)
	}
	if _, _, err := ExtractPart(raw, "9"); err != ErrPartNotFound {
		t.Errorf("ExtractPart(9) err = %v, want ErrPartNotFound", err)
	}
	if got := m.Preview(); got != "Your invoice" {
		t.Errorf("Preview = %q", got)
	}
}

func TestLatin1(t *testing.T) {
	m, _ := parseFile(t, "latin1.eml")
	if m.Subject != "Café crème" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.From.Name != "René Müller" {
		t.Errorf("From = %+v", m.From)
	}
	if strings.TrimSpace(m.Text) != "Grüße aus Köln" {
		t.Errorf("Text = %q", m.Text)
	}
}

func TestMalformed(t *testing.T) {
	m, _ := parseFile(t, "malformed.eml")
	if !m.Date.IsZero() {
		t.Errorf("Date = %v, want zero", m.Date)
	}
	if m.From.Address != "not an address" {
		t.Errorf("From = %+v", m.From)
	}
	var addrs []string
	for _, a := range m.To {
		addrs = append(addrs, a.Address)
	}
	if !reflect.DeepEqual(addrs, []string{"dev@test.local", "broken <", "qa@test.local"}) {
		t.Errorf("To = %+v", m.To)
	}
	// An unknown text subtype is kept as an attachment rather than dropped.
	if len(m.Attachments) != 1 || m.Attachments[0].ContentType != "text/x-unknown" {
		t.Errorf("Attachments = %+v", m.Attachments)
	}
}

func TestHTMLOnlyPreview(t *testing.T) {
	m, _ := parseFile(t, "html-only.eml")
	if m.HasText || !m.HasHTML {
		t.Fatalf("HasText=%v HasHTML=%v", m.HasText, m.HasHTML)
	}
	if got := m.PlainText(); got != "Your code is 482913\n\nIt expires in 10 minutes." {
		t.Errorf("PlainText = %q", got)
	}
	if got := m.Preview(); got != "Your code is 482913 It expires in 10 minutes." {
		t.Errorf("Preview = %q", got)
	}
}

func TestPreviewTruncates(t *testing.T) {
	m := &Message{Text: strings.Repeat("ab ", 200), HasText: true}
	p := m.Preview()
	if !strings.HasSuffix(p, "…") || len([]rune(p)) > PreviewLength+1 {
		t.Errorf("Preview = %q (%d runes)", p, len([]rune(p)))
	}
}

func TestParseAddressList(t *testing.T) {
	got := ParseAddressList(`Dev <DEV@test.local>, "Q, A" <qa@test.local>`)
	want := []Address{{"Dev", "dev@test.local"}, {"Q, A", "qa@test.local"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}
