package mimeparse

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// PreviewLength is the maximum number of characters in a preview.
const PreviewLength = 200

// PlainText returns the readable text of the message: the text part, or the
// HTML part reduced to its visible text.
func (m *Message) PlainText() string {
	if m.HasText && strings.TrimSpace(m.Text) != "" {
		return m.Text
	}
	if m.HasHTML {
		return HTMLToText(m.HTML)
	}
	return ""
}

// Preview is the first PreviewLength characters of the plain text with
// whitespace collapsed.
func (m *Message) Preview() string {
	return truncate(strings.Join(strings.Fields(m.PlainText()), " "), PreviewLength)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// HTMLToText extracts visible text from HTML, skipping head, style and
// script content and separating block elements with newlines.
func HTMLToText(s string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(collapseBlankLines(b.String()))
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head", "style", "script", "title", "noscript", "template":
				if t := z.Token(); t.Type == html.StartTagToken {
					skip++
				}
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table", "section", "article":
				b.WriteByte('\n')
			case "td", "th":
				b.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head", "style", "script", "title", "noscript", "template":
				if skip > 0 {
					skip--
				}
			case "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				b.WriteByte('\n')
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := false
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, l)
		blank = false
	}
	return strings.Join(out, "\n")
}
