package compose

import (
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

func addr(s string) *mail.Address { return &mail.Address{Address: s} }

func TestTextOnly(t *testing.T) {
	raw, err := Build(Message{
		From: addr("me@test.local"), To: []*mail.Address{addr("dev@test.local")},
		Bcc: []*mail.Address{addr("secret@test.local")}, Subject: "Café", Text: "naïve text",
		Date: time.Unix(0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "Content-Transfer-Encoding: quoted-printable") || strings.Contains(s, "naïve") {
		t.Errorf("body not QP-encoded:\n%s", s)
	}
	if !strings.Contains(s, "Subject: =?utf-8?") {
		t.Errorf("subject not encoded:\n%s", s)
	}
	if strings.Contains(s, "secret@test.local") {
		t.Error("Bcc leaked into headers")
	}
	p, _ := mimeparse.Parse(raw)
	if p.Subject != "Café" || strings.TrimSpace(p.Text) != "naïve text" || p.HasHTML {
		t.Errorf("parsed = %+v", p)
	}
}

func TestHTMLOnlyAndAttachments(t *testing.T) {
	raw, err := Build(Message{
		From: addr("me@test.local"), To: []*mail.Address{addr("dev@test.local")},
		Subject: "Report", HTML: "<p>Hi</p>",
		Attachments: []Attachment{{Filename: "report.csv", Data: []byte("a,b\n1,2\n")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.HasText || !p.HasHTML {
		t.Errorf("HTML-only message got a text part: text=%v html=%v", p.HasText, p.HasHTML)
	}
	if len(p.Attachments) != 1 || p.Attachments[0].Filename != "report.csv" || !strings.HasPrefix(p.Attachments[0].ContentType, "text/csv") {
		t.Errorf("attachments = %+v", p.Attachments)
	}
}

func TestTextAndHTML(t *testing.T) {
	raw, err := Build(Message{From: addr("a@x.test"), Cc: []*mail.Address{addr("b@x.test")}, Text: "t", HTML: "<b>h</b>"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := mimeparse.Parse(raw)
	if !p.HasText || !p.HasHTML || len(p.Cc) != 1 {
		t.Errorf("parsed = %+v", p)
	}
}

func TestValidation(t *testing.T) {
	if _, err := Build(Message{To: []*mail.Address{addr("a@x.test")}}); err == nil {
		t.Error("missing From accepted")
	}
	if _, err := Build(Message{From: addr("a@x.test")}); err == nil {
		t.Error("no recipients accepted")
	}
	if _, err := ParseAddresses([]string{"ok@x.test", "not an address"}); err == nil {
		t.Error("bad address accepted")
	}
	list, err := ParseAddresses([]string{"Dev <dev@test.local>"})
	if err != nil || list[0].Name != "Dev" {
		t.Errorf("ParseAddresses = %v, %v", list, err)
	}
	m := Message{To: list, Cc: []*mail.Address{addr("c@x.test")}, Bcc: []*mail.Address{addr("b@x.test")}}
	if got := strings.Join(m.Recipients(), ","); got != "dev@test.local,c@x.test,b@x.test" {
		t.Errorf("Recipients = %s", got)
	}
}

func TestReplyToHeadersAndInlineImages(t *testing.T) {
	raw, err := Build(Message{
		From: addr("me@test.local"), To: []*mail.Address{addr("dev@test.local")},
		ReplyTo: []*mail.Address{{Name: "Support", Address: "help@test.local"}},
		Subject: "Hi", HTML: `<img src="cid:logo">`,
		Headers:     []Header{{"X-Campaign", "welcome"}, {"Subject", "replaced?"}, {"X-Note", "Grüße"}},
		Attachments: []Attachment{{Filename: "logo.png", Data: []byte("png"), ContentID: "logo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{}
	for _, h := range p.Headers {
		headers[h.Name] = h.Value
	}
	if len(p.ReplyTo) != 1 || p.ReplyTo[0].Address != "help@test.local" || p.ReplyTo[0].Name != "Support" {
		t.Errorf("reply-to = %+v", p.ReplyTo)
	}
	if headers["X-Campaign"] != "welcome" || headers["X-Note"] != "Grüße" || p.Subject != "Hi" {
		t.Errorf("headers = %v, subject = %q", headers, p.Subject)
	}
	if len(p.Attachments) != 1 || p.Attachments[0].ContentID != "logo" || !p.Attachments[0].Inline ||
		p.Attachments[0].Filename != "logo.png" {
		t.Errorf("attachments = %+v", p.Attachments)
	}
	if s := string(raw); !strings.Contains(s, "multipart/related") || strings.Contains(s, "multipart/mixed") {
		t.Errorf("inline image alone should make multipart/related, not mixed:\n%s", s)
	}
}

func TestInlineImagesAndFiles(t *testing.T) {
	raw, err := Build(Message{
		From: addr("me@test.local"), To: []*mail.Address{addr("dev@test.local")},
		Subject: "Report", Text: "See attached.", HTML: `<img src="cid:chart">`,
		Attachments: []Attachment{
			{Filename: "chart.png", Data: []byte("png"), ContentID: "chart"},
			{Filename: "report.pdf", Data: []byte("%PDF")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasText || !p.HasHTML || len(p.Attachments) != 2 {
		t.Fatalf("parsed = %+v", p)
	}
	chart, pdf := p.Attachments[0], p.Attachments[1]
	if !chart.Inline || chart.ContentID != "chart" || pdf.Inline || pdf.Filename != "report.pdf" {
		t.Errorf("attachments = %+v", p.Attachments)
	}
	s := string(raw)
	if mixed, related := strings.Index(s, "multipart/mixed"), strings.Index(s, "multipart/related"); mixed < 0 || related < mixed {
		t.Errorf("want multipart/related inside multipart/mixed:\n%s", s)
	}
}

// Header values come from API callers: a line break must not start a new
// header, and invalid names are dropped.
func TestExtraHeadersCannotInject(t *testing.T) {
	raw, err := Build(Message{
		From: addr("me@test.local"), To: []*mail.Address{addr("dev@test.local")}, Subject: "Hi", Text: "x",
		Headers: []Header{{"X-Evil", "a\r\nBcc: victim@test.local"}, {"Bad Name", "x"}, {"X-Entity-Ref-ID", "7"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "\r\nBcc:") || strings.Contains(s, "Bad Name") {
		t.Errorf("header injected:\n%s", s)
	}
	if !strings.Contains(s, "\r\nX-Entity-Ref-ID: 7\r\n") {
		t.Errorf("extra header lost its case:\n%s", s)
	}
}
