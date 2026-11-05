package mimeparse

import "testing"

func TestCRLF(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"a\r\nb\r\n":              "a\r\nb\r\n",
		"a\nb\n":                  "a\r\nb\r\n",
		"a\r\nb":                  "a\r\nb\r\n",
		"a\rb\r\n":                "a\r\nb\r\n",
		"mixed\r\nbare\nend":      "mixed\r\nbare\r\nend\r\n",
		"\n":                      "\r\n",
		"trailing cr\r":           "trailing cr\r\n",
		"Subject: x\n\nbody\n.\n": "Subject: x\r\n\r\nbody\r\n.\r\n",
	}
	for in, want := range cases {
		if got := string(CRLF([]byte(in))); got != want {
			t.Errorf("CRLF(%q) = %q, want %q", in, got, want)
		}
	}

	canonical := []byte("From: a\r\n\r\nbody\r\n")
	if got := CRLF(canonical); &got[0] != &canonical[0] {
		t.Error("canonical input was copied")
	}
}
