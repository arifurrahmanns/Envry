package emulate

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Each provider answers failure simulation with its own error, and nothing
// is captured.
func TestFailureSimulation(t *testing.T) {
	type call struct {
		path, contentType string
		body              any
	}
	sends := map[string]call{
		Resend:   {"/emulate/resend/emails", "application/json", map[string]any{"from": "a@acme.test", "to": "%s", "subject": "x", "text": "x"}},
		SendGrid: {"/emulate/sendgrid/v3/mail/send", "application/json", nil},
		Postmark: {"/emulate/postmark/email", "application/json", map[string]any{"From": "a@acme.test", "To": "%s", "Subject": "x", "TextBody": "x"}},
		Mailgun:  {"/emulate/mailgun/v3/mg.acme.test/messages", "application/x-www-form-urlencoded", nil},
		SES:      {"/emulate/ses/v2/email/outbound-emails", "application/json", nil},
	}
	body := func(provider, to string) any {
		switch provider {
		case SendGrid:
			return map[string]any{
				"personalizations": []map[string]any{{"to": []map[string]string{{"email": to}}}},
				"from":             map[string]string{"email": "a@acme.test"}, "subject": "x",
				"content": []map[string]string{{"type": "text/plain", "value": "x"}},
			}
		case Mailgun:
			return url.Values{"from": {"a@acme.test"}, "to": {to}, "subject": {"x"}, "text": {"x"}}.Encode()
		case SES:
			return map[string]any{
				"FromEmailAddress": "a@acme.test", "Destination": map[string]any{"ToAddresses": []string{to}},
				"Content": map[string]any{"Simple": map[string]any{"Subject": map[string]string{"Data": "x"}, "Body": map[string]any{"Text": map[string]string{"Data": "x"}}}},
			}
		}
		m := map[string]any{}
		for k, v := range sends[provider].body.(map[string]any) {
			m[k] = strings.ReplaceAll(v.(string), "%s", to)
		}
		return m
	}

	refused := map[string]struct {
		status int
		want   string
	}{
		Resend:   {422, `"name":"validation_error"`},
		SendGrid: {400, `"errors":[`},
		Postmark: {422, `"ErrorCode":406`},
		Mailgun:  {400, "failure simulation"},
		SES:      {400, "failure simulation"},
	}
	throttled := map[string]struct {
		status int
		want   string
	}{
		Resend:   {429, `"name":"rate_limit_exceeded"`},
		SendGrid: {429, "too many requests"},
		Postmark: {429, "Too many requests"},
		Mailgun:  {429, "Too many requests"},
		SES:      {429, "Too many requests"},
	}

	for _, provider := range Providers {
		t.Run(provider, func(t *testing.T) {
			e := newEnv(t)
			s := e.server
			s.SetFailures(Failures{RejectRecipients: []string{"*@blocked.test"}})
			c := sends[provider]

			res, b := e.post(c.path, c.contentType, body(provider, "ada@blocked.test"))
			if want := refused[provider]; res.StatusCode != want.status || !strings.Contains(string(b), want.want) {
				t.Errorf("refused: %d %s", res.StatusCode, b)
			}
			if provider == SES && res.Header.Get("X-Amzn-Errortype") != "MessageRejected" {
				t.Errorf("SES error type = %q", res.Header.Get("X-Amzn-Errortype"))
			}

			s.SetFailures(Failures{RejectRate: 100, Roll: func() int { return 0 }})
			res, b = e.post(c.path, c.contentType, body(provider, "ada@test.local"))
			if want := throttled[provider]; res.StatusCode != want.status || !strings.Contains(string(b), want.want) {
				t.Errorf("throttled: %d %s", res.StatusCode, b)
			}
			if e.count() != 0 {
				t.Errorf("%d messages captured", e.count())
			}

			// Allowed recipients still go through, after the delay.
			s.SetFailures(Failures{RejectRecipients: []string{"*@blocked.test"}, Latency: 50 * time.Millisecond})
			start := time.Now()
			res, b = e.post(c.path, c.contentType, body(provider, "ada@test.local"))
			if res.StatusCode >= 300 || e.count() != 1 || time.Since(start) < 50*time.Millisecond {
				t.Errorf("allowed: %d %s after %v", res.StatusCode, b, time.Since(start))
			}
		})
	}
}

// In a Postmark batch a refused recipient fails only its own message.
func TestPostmarkBatchRefusal(t *testing.T) {
	e := newEnv(t)
	e.server.SetFailures(Failures{RejectRecipients: []string{"bounced@*"}})
	res, b := e.post("/emulate/postmark/email/batch", "application/json", []map[string]any{
		{"From": "a@acme.test", "To": "ada@test.local", "Subject": "1", "TextBody": "x"},
		{"From": "a@acme.test", "To": "bounced@test.local", "Subject": "2", "TextBody": "x"},
	})
	if res.StatusCode != 200 || !strings.Contains(string(b), `"ErrorCode":0`) || !strings.Contains(string(b), `"ErrorCode":406`) || e.count() != 1 {
		t.Errorf("%d %s, %d captured", res.StatusCode, b, e.count())
	}
}

// SES's query API reports throttling as a 400 with code Throttling.
func TestSESv1Throttling(t *testing.T) {
	e := newEnv(t)
	e.server.SetFailures(Failures{RejectRate: 100, Roll: func() int { return 0 }})
	form := url.Values{"Action": {"SendEmail"}, "Source": {"a@acme.test"}, "Destination.ToAddresses.member.1": {"ada@test.local"},
		"Message.Subject.Data": {"x"}, "Message.Body.Text.Data": {"x"}}
	res, b := e.post("/emulate/ses/", "application/x-www-form-urlencoded", form.Encode())
	if res.StatusCode != 400 || !strings.Contains(string(b), "<Code>Throttling</Code>") {
		t.Errorf("%d %s", res.StatusCode, b)
	}
}
