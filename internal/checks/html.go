package checks

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// document holds what the checks need from the HTML part.
type document struct {
	anchors []anchor
	images  []image
}

type anchor struct {
	href string // trimmed href attribute
	text string // visible text, or the alt text of an image inside the link
}

type image struct {
	src    string
	hasAlt bool // alt="" counts: it marks a decorative image
	tiny   bool // 0 or 1 pixel wide and high, e.g. a tracking pixel
}

const maxLinkText = 80

func scanHTML(s string) document {
	var doc document
	if s == "" {
		return doc
	}
	z := html.NewTokenizer(strings.NewReader(s))
	var cur *anchor
	var text strings.Builder
	finish := func() {
		if cur != nil {
			cur.text = truncate(strings.Join(strings.Fields(text.String()), " "), maxLinkText)
			doc.anchors = append(doc.anchors, *cur)
			cur = nil
		}
		text.Reset()
	}

	for {
		switch z.Next() {
		case html.ErrorToken:
			finish()
			return doc
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.DataAtom {
			case atom.A:
				finish()
				if href, ok := attr(t, "href"); ok {
					cur = &anchor{href: strings.TrimSpace(href)}
				}
			case atom.Img:
				src, _ := attr(t, "src")
				alt, hasAlt := attr(t, "alt")
				w, _ := attr(t, "width")
				h, _ := attr(t, "height")
				doc.images = append(doc.images, image{src: strings.TrimSpace(src), hasAlt: hasAlt, tiny: tiny(w) && tiny(h)})
				if cur != nil && alt != "" {
					text.WriteString(" " + alt + " ")
				}
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "a" {
				finish()
			}
		case html.TextToken:
			if cur != nil {
				text.Write(z.Text())
			}
		}
	}
}

func attr(t html.Token, name string) (string, bool) {
	for _, a := range t.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// tiny reports a width or height attribute of at most one pixel.
func tiny(v string) bool {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "px"))
	return err == nil && n <= 1
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
