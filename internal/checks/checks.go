// Package checks inspects a captured message for problems that hurt how it
// renders or whether it reaches the inbox: a missing plain-text part, images
// without alt text, HTML large enough for Gmail to clip, broken or risky
// links and missing or spam-like headers. Link reachability is checked
// separately with CheckLinks, only when the user asks for it.
package checks

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// Status is the outcome of one check, from best to worst.
type Status string

const (
	StatusPass Status = "pass"
	StatusInfo Status = "info"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

var severity = map[Status]int{StatusPass: 0, StatusInfo: 1, StatusWarn: 2, StatusFail: 3}

// Check is one finding. Details lists the individual problems, if any.
type Check struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Status  Status   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details"`
}

// Link is an http(s) link found in the message, checkable with CheckLinks.
type Link struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

// Report is the result of Run.
type Report struct {
	Checks []Check `json:"checks"`
	Links  []Link  `json:"links"`
}

const (
	// GmailClipBytes is the HTML size above which Gmail clips a message
	// and hides the rest behind "View entire message".
	GmailClipBytes = 102 * 1024
	// largeMessageBytes is where providers start rejecting or delaying mail.
	largeMessageBytes = 10 << 20
	// longSubject is roughly what inbox lists show before cutting off.
	longSubject = 70
)

// Run analyses a parsed message; size is the raw message size in bytes.
func Run(m *mimeparse.Message, size int) Report {
	doc := scanHTML(m.HTML)
	links, linkCheck := checkLinks(m, doc)

	var checks []Check
	checks = append(checks, textPart(m))
	if m.HasHTML {
		checks = append(checks, htmlSize(m))
	}
	if c, ok := imageAlt(doc); ok {
		checks = append(checks, c)
	}
	if linkCheck != nil {
		checks = append(checks, *linkCheck)
	}
	checks = append(checks, requiredHeaders(m), subjectLine(m.Subject), unsubscribe(m), messageSize(size))

	for i := range checks {
		if checks[i].Details == nil {
			checks[i].Details = []string{}
		}
	}
	if links == nil {
		links = []Link{}
	}
	return Report{Checks: checks, Links: links}
}

func textPart(m *mimeparse.Message) Check {
	c := Check{ID: "text-part", Title: "Plain-text version"}
	switch {
	case !m.HasHTML:
		c.Status, c.Message = StatusPass, "Plain-text message; every client can display it."
	case !m.HasText:
		c.Status = StatusFail
		c.Message = "HTML only, with no text/plain alternative. Spam filters penalise this, and text-only clients show nothing useful."
	case strings.TrimSpace(m.Text) == "":
		c.Status, c.Message = StatusWarn, "The text/plain part is empty."
	default:
		c.Status, c.Message = StatusPass, "Includes both HTML and plain-text versions."
	}
	return c
}

func htmlSize(m *mimeparse.Message) Check {
	n := len(m.HTML)
	c := Check{ID: "gmail-clipping", Title: "Gmail clipping"}
	if n > GmailClipBytes {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("HTML is %s. Gmail clips messages over 102 KB and hides the rest behind \"View entire message\".", formatBytes(n))
	} else {
		c.Status = StatusPass
		c.Message = fmt.Sprintf("HTML is %s, under Gmail's 102 KB clipping limit.", formatBytes(n))
	}
	return c
}

func imageAlt(doc document) (Check, bool) {
	var counted int
	var missing []string
	for _, img := range doc.images {
		if img.tiny {
			continue // tracking pixels and spacers need no alt text
		}
		counted++
		if !img.hasAlt {
			missing = append(missing, describeSrc(img.src))
		}
	}
	if counted == 0 {
		return Check{}, false
	}
	c := Check{ID: "image-alt", Title: "Image alt text"}
	if len(missing) == 0 {
		c.Status, c.Message = StatusPass, fmt.Sprintf("All %d %s have alt text.", counted, plural(counted, "image", "images"))
		return c, true
	}
	c.Status = StatusWarn
	c.Message = fmt.Sprintf("%d of %d %s %s no alt attribute. Clients that block images show nothing in their place, and screen readers read the file name.",
		len(missing), counted, plural(counted, "image", "images"), plural(len(missing), "has", "have"))
	c.Details = missing
	return c, true
}

var textURL = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+`)

// checkLinks collects the message's http(s) links and reports risky ones.
func checkLinks(m *mimeparse.Message, doc document) ([]Link, *Check) {
	var links []Link
	seen := map[string]bool{}
	add := func(u, text string) {
		if !seen[u] {
			seen[u] = true
			links = append(links, Link{URL: u, Text: text})
		}
	}

	type finding struct {
		status Status
		text   string
	}
	var findings []finding
	total := 0

	inspect := func(href, text string) {
		total++
		label := href
		if text != "" {
			label = fmt.Sprintf("%s (%q)", href, text)
		}
		lower := strings.ToLower(href)
		switch {
		case href == "" || href == "#":
			findings = append(findings, finding{StatusWarn, fmt.Sprintf("Empty link: %q", text)})
			return
		case strings.HasPrefix(lower, "javascript:"):
			findings = append(findings, finding{StatusFail, "Script link, blocked by every mail client: " + label})
			return
		case strings.HasPrefix(href, "#"):
			return // in-message anchor
		}
		u, err := url.Parse(href)
		if err != nil {
			findings = append(findings, finding{StatusFail, "Malformed URL: " + label})
			return
		}
		switch {
		case u.Scheme == "" && (u.Host != "" || strings.HasPrefix(href, "//")):
			findings = append(findings, finding{StatusWarn, "Protocol-relative link; some clients open it as file://: " + label})
		case u.Scheme == "":
			findings = append(findings, finding{StatusFail, "Relative link; email has no base URL, so it goes nowhere: " + label})
		case u.Scheme == "http" || u.Scheme == "https":
			add(href, text)
			if local(u.Hostname()) {
				findings = append(findings, finding{StatusInfo, "Points to a local address recipients cannot reach: " + label})
			} else if u.Scheme == "http" {
				findings = append(findings, finding{StatusWarn, "Insecure http:// link: " + label})
			}
		}
	}

	for _, a := range doc.anchors {
		inspect(a.href, a.text)
	}
	if !m.HasHTML {
		for _, u := range textURL.FindAllString(m.Text, -1) {
			inspect(strings.TrimRight(u, ".,;:!?"), "")
		}
	}
	if total == 0 {
		return links, nil
	}

	c := Check{ID: "links", Title: "Links", Status: StatusPass}
	for _, f := range findings {
		if severity[f.status] > severity[c.Status] {
			c.Status = f.status
		}
		c.Details = append(c.Details, f.text)
	}
	if len(findings) == 0 {
		c.Message = fmt.Sprintf("%d %s, none with obvious problems. Use Check links to request them.", total, plural(total, "link", "links"))
	} else {
		c.Message = fmt.Sprintf("%d of %d %s %s attention.", len(findings), total, plural(total, "link", "links"), plural(len(findings), "needs", "need"))
	}
	return links, &c
}

// local reports whether host only resolves on the sender's machine or network.
func local(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".test") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())
}

func requiredHeaders(m *mimeparse.Message) Check {
	c := Check{ID: "headers", Title: "Required headers", Status: StatusPass}
	if m.From.Address == "" {
		c.Status = StatusFail
		c.Details = append(c.Details, "From is missing or has no valid address.")
	}
	if !hasHeader(m, "Date") {
		c.Status = worse(c.Status, StatusWarn)
		c.Details = append(c.Details, "Date is missing; clients sort the message by arrival time or show no date.")
	}
	if !hasHeader(m, "Message-ID") {
		c.Status = worse(c.Status, StatusWarn)
		c.Details = append(c.Details, "Message-ID is missing; spam filters penalise this and replies cannot thread.")
	}
	if c.Status == StatusPass {
		c.Message = "From, Date and Message-ID are present."
	} else {
		c.Message = fmt.Sprintf("%d %s missing.", len(c.Details), plural(len(c.Details), "header is", "headers are"))
	}
	return c
}

// spamWords are classic content-filter triggers; matching is a hint only.
var spamWords = []string{
	"act now", "100% free", "buy now", "cash bonus", "click here", "congratulations", "double your",
	"earn money", "free gift", "guaranteed", "limited time", "make money", "no cost", "risk-free",
	"urgent", "winner", "you have been selected", "$$$",
}

func subjectLine(subject string) Check {
	c := Check{ID: "subject", Title: "Subject line", Status: StatusPass}
	s := strings.TrimSpace(subject)
	if s == "" {
		c.Status, c.Message = StatusWarn, "The subject is empty; spam filters and recipients both treat this as suspicious."
		return c
	}
	if shouting(s) {
		c.Status = worse(c.Status, StatusWarn)
		c.Details = append(c.Details, "Written in capitals, a common spam signal.")
	}
	if strings.Contains(s, "!!") || strings.Contains(s, "??") {
		c.Status = worse(c.Status, StatusWarn)
		c.Details = append(c.Details, "Repeated punctuation (!! or ??), a common spam signal.")
	}
	lower := strings.ToLower(s)
	var words []string
	for _, w := range spamWords {
		if strings.Contains(lower, w) {
			words = append(words, w)
		}
	}
	if len(words) > 0 {
		c.Status = worse(c.Status, StatusInfo)
		c.Details = append(c.Details, "Contains phrases spam filters weigh: "+strings.Join(words, ", ")+".")
	}
	if n := len([]rune(s)); n > longSubject {
		c.Status = worse(c.Status, StatusInfo)
		c.Details = append(c.Details, fmt.Sprintf("%d characters; inbox lists usually cut off around %d.", n, longSubject))
	}
	if c.Status == StatusPass {
		c.Message = "No problems found."
	} else {
		c.Message = fmt.Sprintf("%d possible %s.", len(c.Details), plural(len(c.Details), "issue", "issues"))
	}
	return c
}

// shouting reports a subject with at least five letters and no lower case.
func shouting(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			letters++
		}
	}
	return letters >= 5
}

func unsubscribe(m *mimeparse.Message) Check {
	c := Check{ID: "list-unsubscribe", Title: "Unsubscribe header"}
	switch {
	case !hasHeader(m, "List-Unsubscribe"):
		c.Status = StatusInfo
		c.Message = "No List-Unsubscribe header. Gmail and Yahoo require one-click unsubscribe for bulk and marketing mail; transactional mail can skip it."
	case !hasHeader(m, "List-Unsubscribe-Post"):
		c.Status = StatusInfo
		c.Message = "List-Unsubscribe is set, but without List-Unsubscribe-Post: List-Unsubscribe=One-Click (RFC 8058) for one-click unsubscribe."
	default:
		c.Status, c.Message = StatusPass, "One-click unsubscribe headers are set."
	}
	return c
}

func messageSize(size int) Check {
	c := Check{ID: "size", Title: "Message size", Status: StatusPass}
	if size > largeMessageBytes {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("%s. Many providers reject or delay messages over 10 MB; link to large files instead.", formatBytes(size))
	} else {
		c.Message = formatBytes(size) + " in total."
	}
	return c
}

func hasHeader(m *mimeparse.Message, name string) bool {
	for _, h := range m.Headers {
		if strings.EqualFold(h.Name, name) && strings.TrimSpace(h.Value) != "" {
			return true
		}
	}
	return false
}

func worse(a, b Status) Status {
	if severity[b] > severity[a] {
		return b
	}
	return a
}

func describeSrc(src string) string {
	switch {
	case src == "":
		return "<img> with no src"
	case strings.HasPrefix(src, "data:"):
		return "Embedded data: image"
	case len(src) > 120:
		return src[:117] + "..."
	}
	return src
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
