package mimeparse

import "bytes"

var crlf = []byte("\r\n")

// CRLF returns raw with every line ending normalised to CRLF and a trailing
// CRLF: the form POP3 and IMAP clients receive and whose length they are
// told. Already-canonical input (anything that arrived over SMTP) is
// returned as is, without copying.
func CRLF(raw []byte) []byte {
	if len(raw) == 0 || isCanonical(raw) {
		return raw
	}
	var b bytes.Buffer
	b.Grow(len(raw) + len(raw)/32 + 2)
	for i := 0; i < len(raw); i++ {
		switch c := raw[i]; c {
		case '\r':
			b.Write(crlf)
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
		case '\n':
			b.Write(crlf)
		default:
			b.WriteByte(c)
		}
	}
	if !bytes.HasSuffix(b.Bytes(), crlf) {
		b.Write(crlf)
	}
	return b.Bytes()
}

func isCanonical(raw []byte) bool {
	if !bytes.HasSuffix(raw, crlf) {
		return false
	}
	for i, c := range raw {
		switch c {
		case '\n':
			if i == 0 || raw[i-1] != '\r' {
				return false
			}
		case '\r':
			if i+1 >= len(raw) || raw[i+1] != '\n' {
				return false
			}
		}
	}
	return true
}
