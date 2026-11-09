package extract

import (
	"strings"
	"testing"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// message builds a parsed message from a subject and parts given as
// media type and body.
func message(t *testing.T, subject string, parts ...[2]string) *mimeparse.Message {
	t.Helper()
	var b strings.Builder
	b.WriteString("From: App <app@example.test>\r\nTo: dev@test.local\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\n")
	if len(parts) == 1 {
		b.WriteString("Content-Type: " + parts[0][0] + "; charset=utf-8\r\n\r\n" + parts[0][1])
	} else {
		b.WriteString("Content-Type: multipart/alternative; boundary=b\r\n\r\n")
		for _, p := range parts {
			b.WriteString("--b\r\nContent-Type: " + p[0] + "; charset=utf-8\r\n\r\n" + p[1] + "\r\n")
		}
		b.WriteString("--b--\r\n")
	}
	m, err := mimeparse.Parse([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func text(s string) [2]string { return [2]string{"text/plain", s} }
func htm(s string) [2]string  { return [2]string{"text/html", s} }

func values(codes []Code) []string {
	out := []string{}
	for _, c := range codes {
		out = append(out, c.Value)
	}
	return out
}

func TestCodes(t *testing.T) {
	tests := []struct {
		name    string
		msg     func(t *testing.T) *mimeparse.Message
		want    []string
		context string
	}{
		{
			name: "Clerk-style code on its own line",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "482913 is your verification code", text(
					"Verification code\n\nEnter the following verification code when prompted:\n\n482913\n\n"+
						"To protect your account, do not share this code.\n\n© 2026 Acme Inc."))
			},
			want:    []string{"482913"},
			context: "482913 is your verification code",
		},
		{
			name: "code in the sentence",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Sign in to Acme", text("Your one-time code is 731044. It expires in 10 minutes."))
			},
			want:    []string{"731044"},
			context: "Your one-time code is 731044. It expires in 10 minutes.",
		},
		{
			name: "code after a colon",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Security alert", text("Security code:5531\nIf this wasn't you, reset your password."))
			},
			want: []string{"5531"},
		},
		{
			name: "Slack-style letters and digits",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Slack confirmation code", text(
					"Your confirmation code is below — enter it in your open browser window.\n\nQ7X-B2M\n\nThanks, The Slack Team"))
			},
			want:    []string{"Q7X-B2M"},
			context: "Your confirmation code is below — enter it in your open browser window.",
		},
		{
			name: "one digit per table cell",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Welcome", htm(`<html><body><p>Here is your login code</p>
					<table><tr><td>4</td><td>8</td><td>2</td><td>9</td><td>1</td><td>3</td></tr></table>
					<p>It is valid for 15 minutes.</p></body></html>`))
			},
			want: []string{"482913"},
		},
		{
			name: "123 456 with a space",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Your Acme code", text("Use this code to finish signing in:\n\n123 456\n"))
			},
			want: []string{"123456"},
		},
		{
			name: "a year named as the code",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Hi", text("Your PIN is 2024."))
			},
			want: []string{"2024"},
		},
		{
			name: "Laravel password reset has no code",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Reset Password Notification", text(
					"Hello!\n\nYou are receiving this email because we received a password reset request for your account.\n\n"+
						"Reset Password: http://localhost/reset-password/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08?email=taylor%40example.com\n\n"+
						"This password reset link will expire in 60 minutes.\n\nIf you did not request a password reset, no further action is required.\n\n"+
						"Regards,\nLaravel\n\n© 2026 Laravel. All rights reserved."))
			},
			want: []string{},
		},
		{
			name: "order receipt",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Order #10234 confirmed", text(
					"Thanks for your order #10234.\nTotal: $1,249.00 (incl. 20% VAT)\nYour discount code SAVE20 was applied.\n"+
						"Delivery on 2026-11-12 at 10:30.\nQuestions? Call +1 555 123 4567 or reply to this email."))
			},
			want: []string{},
		},
		{
			name: "newsletter with dates and a host:port",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "This week at Acme", text(
					"Join our webinar on November 12, 2026 at 3:00 PM.\nUse code WEBINAR to register.\n"+
						"Try the demo at localhost:3000 or 127.0.0.1:8025.\nVersion 2.14.1 is out."))
			},
			want: []string{},
		},
		{
			name: "security alert with a phone number",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "New sign-in to your account", text(
					"We noticed a new sign-in from Chrome on Windows.\nIf you didn't request this code, call 555-123-4567.\nRef 20261109."))
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Find(tt.msg(t)).Codes
			if strings.Join(values(got), ",") != strings.Join(tt.want, ",") {
				t.Fatalf("codes = %+v, want %v", got, tt.want)
			}
			if tt.context != "" && got[0].Context != tt.context {
				t.Errorf("context = %q, want %q", got[0].Context, tt.context)
			}
		})
	}
}

func TestLinks(t *testing.T) {
	const resetURL = "http://localhost/reset-password/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08?email=taylor%40example.com"
	const verifyURL = "http://localhost/email/verify/1/5baa61e4c9b93f3f0682250b6cf8331b7ee68fd8?expires=1794200000&signature=0b1c8a8e2f4d"
	const nextAuthURL = "http://localhost:3000/api/auth/callback/nodemailer?callbackUrl=http%3A%2F%2Flocalhost%3A3000&token=8f2c5e1d7a9b3c4f6e0d2a1b&email=dev%40test.local"

	tests := []struct {
		name string
		msg  func(t *testing.T) *mimeparse.Message
		want []Link
	}{
		{
			name: "Laravel password reset",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Reset Password Notification",
					text("Reset Password: "+resetURL+"\n\nRegards,\nLaravel"),
					htm(`<a href="http://localhost">Laravel</a>
						<p>You are receiving this email because we received a password reset request for your account.</p>
						<a href="`+strings.ReplaceAll(resetURL, "&", "&amp;")+`" class="button">Reset Password</a>
						<p>If you're having trouble clicking the "Reset Password" button, copy and paste the URL below:
						<a href="`+resetURL+`">`+resetURL+`</a></p>`))
			},
			want: []Link{{URL: resetURL, Kind: KindReset, Text: "Reset Password"}},
		},
		{
			name: "Laravel email verification",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Verify Email Address", htm(`<a href="`+strings.ReplaceAll(verifyURL, "&", "&amp;")+`">Verify Email Address</a>
					<a href="https://laravel.com/privacy">Privacy</a>`))
			},
			want: []Link{{URL: verifyURL, Kind: KindVerify, Text: "Verify Email Address"}},
		},
		{
			name: "Auth.js magic link",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "Sign in to localhost:3000",
					text("Sign in to localhost:3000\n"+nextAuthURL+"\n"),
					htm(`<p>Sign in to <strong>localhost:3000</strong></p><a href="`+strings.ReplaceAll(nextAuthURL, "&", "&amp;")+`">Sign in</a>`))
			},
			want: []Link{{URL: nextAuthURL, Kind: KindLogin, Text: "Sign in"}},
		},
		{
			name: "Supabase magic link says verify in its URL",
			msg: func(t *testing.T) *mimeparse.Message {
				const u = "https://abc.supabase.co/auth/v1/verify?token=pkce_4f1d2b8a9c7e6f5a&type=magiclink&redirect_to=http://localhost:3000"
				return message(t, "Your Magic Link", htm(`<h2>Magic Link</h2><p><a href="`+u+`">Log In</a></p>`))
			},
			want: []Link{{
				URL:  "https://abc.supabase.co/auth/v1/verify?token=pkce_4f1d2b8a9c7e6f5a&type=magiclink&redirect_to=http://localhost:3000",
				Kind: KindLogin, Text: "Log In",
			}},
		},
		{
			name: "invitation",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "You're invited", htm(`<a href="https://app.example.com/invitations/accept?ticket=a1b2c3d4e5f6a7b8c9">Accept invitation</a>
					<a href="https://discord.gg/acme">Join us on Discord</a>`))
			},
			want: []Link{{URL: "https://app.example.com/invitations/accept?ticket=a1b2c3d4e5f6a7b8c9", Kind: KindInvite, Text: "Accept invitation"}},
		},
		{
			name: "newsletter has no action links",
			msg: func(t *testing.T) *mimeparse.Message {
				return message(t, "This week at Acme", htm(`<a href="https://acme.test/login">Log in</a>
					<a href="https://acme.test/blog/post-1">Read more</a>
					<a href="https://acme.test/unsubscribe?token=9a8b7c6d5e4f3a2b1c0d">Unsubscribe</a>
					<a href="https://acme.test/email/preferences?sig=1a2b3c4d5e6f7a8b9c0d">Email preferences</a>`))
			},
			want: []Link{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Find(tt.msg(t)).Links
			if len(got) != len(tt.want) {
				t.Fatalf("links = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("link %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
