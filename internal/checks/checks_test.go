package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"

	"github.com/arifurrahmanns/envry/internal/compose"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

func build(t *testing.T, m compose.Message) (*mimeparse.Message, int) {
	t.Helper()
	if m.From == nil {
		m.From = &mail.Address{Address: "app@example.com"}
	}
	if len(m.To) == 0 {
		m.To = []*mail.Address{{Address: "dev@test.local"}}
	}
	raw, err := compose.Build(m)
	if err != nil {
		t.Fatal(err)
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p, len(raw)
}

func parse(t *testing.T, raw string) (*mimeparse.Message, int) {
	t.Helper()
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	p, err := mimeparse.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p, len(raw)
}

func find(t *testing.T, r Report, id string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, r.Checks)
	return Check{}
}

func has(r Report, id string) bool {
	for _, c := range r.Checks {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestTextPart(t *testing.T) {
	cases := []struct {
		name string
		msg  compose.Message
		want Status
	}{
		{"text only", compose.Message{Subject: "Hi", Text: "Hello"}, StatusPass},
		{"html and text", compose.Message{Subject: "Hi", Text: "Hello", HTML: "<p>Hello</p>"}, StatusPass},
		{"html only", compose.Message{Subject: "Hi", HTML: "<p>Hello</p>"}, StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, size := build(t, tc.msg)
			if got := find(t, Run(p, size), "text-part").Status; got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGmailClipping(t *testing.T) {
	p, size := build(t, compose.Message{Subject: "Small", Text: "x", HTML: "<p>small</p>"})
	if c := find(t, Run(p, size), "gmail-clipping"); c.Status != StatusPass {
		t.Errorf("small HTML: %+v", c)
	}
	big := "<p>" + strings.Repeat("Lorem ipsum dolor sit amet. ", 4000) + "</p>"
	p, size = build(t, compose.Message{Subject: "Big", Text: "x", HTML: big})
	if c := find(t, Run(p, size), "gmail-clipping"); c.Status != StatusWarn || !strings.Contains(c.Message, "102 KB") {
		t.Errorf("big HTML: %+v", c)
	}
	p, size = build(t, compose.Message{Subject: "Text", Text: "x"})
	if has(Run(p, size), "gmail-clipping") {
		t.Error("text-only message has a clipping check")
	}
}

func TestImageAlt(t *testing.T) {
	html := `<img src="https://cdn.example.com/logo.png" alt="Acme">
<img src="https://cdn.example.com/hero.jpg">
<img src="https://cdn.example.com/spacer.gif" alt="">
<img src="https://t.example.com/open.gif" width="1" height="1">`
	p, size := build(t, compose.Message{Subject: "Images", Text: "x", HTML: html})
	c := find(t, Run(p, size), "image-alt")
	if c.Status != StatusWarn || len(c.Details) != 1 || c.Details[0] != "https://cdn.example.com/hero.jpg" {
		t.Errorf("check = %+v", c)
	}
	if !strings.HasPrefix(c.Message, "1 of 3 images") {
		t.Errorf("message = %q (the tracking pixel should not count)", c.Message)
	}

	p, size = build(t, compose.Message{Subject: "No images", Text: "x", HTML: "<p>Hi</p>"})
	if has(Run(p, size), "image-alt") {
		t.Error("message without images has an alt-text check")
	}
}

func TestLinks(t *testing.T) {
	html := `<a href="https://example.com/ok">Fine</a>
<a href="https://example.com/ok">Same link twice</a>
<a href="http://example.com/insecure">Insecure</a>
<a href="http://localhost:3000/verify?token=abc">Verify email</a>
<a href="/reset-password">Reset</a>
<a href="javascript:alert(1)">Script</a>
<a href="#">Nothing</a>
<a href="mailto:help@example.com">Mail us</a>
<a href="#top">Back to top</a>
<a name="anchor">Not a link</a>
<a href="https://example.com/logo"><img src="logo.png" alt="Acme logo"></a>`
	p, size := build(t, compose.Message{Subject: "Links", Text: "x", HTML: html})
	r := Run(p, size)

	c := find(t, r, "links")
	if c.Status != StatusFail {
		t.Errorf("status = %s, want fail", c.Status)
	}
	wantDetails := []string{"Insecure http://", "local address", "Relative link", "Script link", "Empty link"}
	if len(c.Details) != len(wantDetails) {
		t.Fatalf("details = %q", c.Details)
	}
	for i, want := range wantDetails {
		if !strings.Contains(c.Details[i], want) {
			t.Errorf("detail %d = %q, want it to mention %q", i, c.Details[i], want)
		}
	}

	var urls []string
	for _, l := range r.Links {
		urls = append(urls, l.URL)
	}
	want := "https://example.com/ok http://example.com/insecure http://localhost:3000/verify?token=abc https://example.com/logo"
	if got := strings.Join(urls, " "); got != want {
		t.Errorf("links = %s\nwant    %s", got, want)
	}
	if r.Links[3].Text != "Acme logo" {
		t.Errorf("image link text = %q, want the alt text", r.Links[3].Text)
	}
}

func TestLinksInPlainText(t *testing.T) {
	p, size := build(t, compose.Message{Subject: "Text links", Text: "Reset at https://example.com/reset?t=1. Or visit (https://example.com/help)."})
	r := Run(p, size)
	if len(r.Links) != 2 || r.Links[0].URL != "https://example.com/reset?t=1" || r.Links[1].URL != "https://example.com/help" {
		t.Errorf("links = %+v", r.Links)
	}
	if c := find(t, r, "links"); c.Status != StatusPass {
		t.Errorf("check = %+v", c)
	}
}

func TestHeaders(t *testing.T) {
	p, size := build(t, compose.Message{Subject: "Hi", Text: "x"})
	if c := find(t, Run(p, size), "headers"); c.Status != StatusPass {
		t.Errorf("composed message: %+v", c)
	}

	p, size = parse(t, "To: dev@test.local\nSubject: Hi\n\nbody\n")
	c := find(t, Run(p, size), "headers")
	if c.Status != StatusFail || len(c.Details) != 3 {
		t.Errorf("bare message: %+v", c)
	}
}

func TestSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    Status
		detail  string
	}{
		{"Your order has shipped", StatusPass, ""},
		{"", StatusWarn, ""},
		{"FINAL NOTICE ABOUT YOUR ACCOUNT", StatusWarn, "capitals"},
		{"Big savings inside!!!", StatusWarn, "punctuation"},
		{"Congratulations, you are a winner", StatusInfo, "congratulations, winner"},
		{"Order 12345: " + strings.Repeat("a long product name ", 5), StatusInfo, "characters"},
		{"ID 12345", StatusPass, ""},
	}
	for _, tc := range cases {
		c := subjectLine(tc.subject)
		if c.Status != tc.want || (tc.detail != "" && !strings.Contains(strings.Join(c.Details, " "), tc.detail)) {
			t.Errorf("subject %q: %+v, want %s mentioning %q", tc.subject, c, tc.want, tc.detail)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	p, size := parse(t, "From: a@example.com\nList-Unsubscribe: <https://example.com/u>\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\n\nbody\n")
	if c := find(t, Run(p, size), "list-unsubscribe"); c.Status != StatusPass {
		t.Errorf("both headers: %+v", c)
	}
	p, size = parse(t, "From: a@example.com\n\nbody\n")
	if c := find(t, Run(p, size), "list-unsubscribe"); c.Status != StatusInfo {
		t.Errorf("no headers: %+v", c)
	}
}

func TestMessageSize(t *testing.T) {
	if c := messageSize(2048); c.Status != StatusPass || c.Message != "2.0 KB in total." {
		t.Errorf("small: %+v", c)
	}
	if c := messageSize(12 << 20); c.Status != StatusWarn {
		t.Errorf("large: %+v", c)
	}
}

func TestReportIsNeverNull(t *testing.T) {
	p, size := build(t, compose.Message{Subject: "Hi", Text: "no links"})
	r := Run(p, size)
	if r.Links == nil {
		t.Error("Links is nil")
	}
	for _, c := range r.Checks {
		if c.Details == nil {
			t.Errorf("%s: Details is nil", c.ID)
		}
	}
}

func TestCheckLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusFound)
	})
	mux.HandleFunc("/get-only", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/agent", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "Envry-LinkCheck/") {
			w.WriteHeader(http.StatusForbidden)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	urls := []string{srv.URL + "/ok", srv.URL + "/missing", srv.URL + "/moved", srv.URL + "/get-only", srv.URL + "/agent", closedURL + "/x", "ftp://example.com/file"}
	got := CheckLinks(context.Background(), srv.Client(), urls)
	want := []struct {
		status int
		ok     bool
		err    string
	}{
		{200, true, ""},
		{404, false, ""},
		{200, true, ""},
		{200, true, ""},
		{200, true, ""},
		{0, false, "could not connect"},
		{0, false, "not an http(s) URL"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results", len(got))
	}
	for i, w := range want {
		g := got[i]
		if g.URL != urls[i] || g.Status != w.status || g.OK != w.ok || g.Error != w.err {
			t.Errorf("%s: %+v, want status %d ok %v error %q", urls[i], g, w.status, w.ok, w.err)
		}
	}
}

func TestCheckLinksLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	urls := make([]string, MaxLinks+10)
	for i := range urls {
		urls[i] = srv.URL
	}
	if got := CheckLinks(context.Background(), srv.Client(), urls); len(got) != MaxLinks {
		t.Errorf("checked %d links, want %d", len(got), MaxLinks)
	}
}
