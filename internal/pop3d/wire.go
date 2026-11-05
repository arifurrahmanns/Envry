package pop3d

import (
	"bufio"
	"bytes"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

func crlf(raw []byte) []byte { return mimeparse.CRLF(raw) }

// writeDotStuffed writes a CRLF-normalised message as a multi-line response:
// lines starting with "." get an extra "." and the response ends with
// ".\r\n". With bodyLines >= 0 it writes only the headers, the blank line
// and that many body lines (the TOP command).
func writeDotStuffed(w *bufio.Writer, data []byte, bodyLines int) {
	inBody := false
	written := 0
	for len(data) > 0 {
		i := bytes.Index(data, []byte("\r\n"))
		var line []byte
		if i < 0 {
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+2:]
		}
		if inBody && bodyLines >= 0 {
			if written == bodyLines {
				break
			}
			written++
		}
		if len(line) > 0 && line[0] == '.' {
			w.WriteByte('.')
		}
		w.Write(line)
		w.WriteString("\r\n")
		if !inBody && len(line) == 0 {
			inBody = true
		}
	}
	w.WriteString(".\r\n")
}
