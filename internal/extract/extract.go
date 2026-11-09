// Package extract finds what a developer usually opens a test email for: a
// one-time code, and the link that verifies an address, resets a password,
// signs in or accepts an invitation. It works on the parsed message alone
// and makes no network requests.
package extract

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// The JSON shapes below are part of the /api/v1 contract mirrored by
// web/src/lib/api.ts; keep the two in sync.

// Result is what was found in one message, best match first.
type Result struct {
	Codes []Code `json:"codes"`
	Links []Link `json:"links"`
}

// Code is a one-time code such as 482913 or Q7X-B2M.
type Code struct {
	Value string `json:"value"`
	// Context is the sentence that names the code, e.g. "Your verification
	// code is:", so people can see why it was picked.
	Context string `json:"context"`
}

// Link is an action link.
type Link struct {
	URL string `json:"url"`
	// Kind is what the link does: verify, reset, login or invite.
	Kind string `json:"kind"`
	// Text is the link's visible text, empty for a bare URL.
	Text string `json:"text"`
}

// Link kinds.
const (
	KindVerify = "verify"
	KindReset  = "reset"
	KindLogin  = "login"
	KindInvite = "invite"
)

// At most this many codes and links are returned.
const maxResults = 3

// Find returns the message's one-time codes and action links.
func Find(m *mimeparse.Message) Result {
	return Result{Codes: codes(m), Links: links(m)}
}

// ---- Codes ----

var (
	// Words that name a one-time code. "code" also covers French, German
	// and Dutch.
	strongWord = regexp.MustCompile(`(?i)\b(codes?|otp|one[- ]?time|passcode|pass code|pin|2fa|mfa|token|c[oó]digo|codice|kode)\b`)
	// Words that often surround one.
	weakWord = regexp.MustCompile(`(?i)\b(verification|verify|verifying|confirm|confirmation|log ?in|sign[- ]?in|authenticat\w*|security)\b`)
	// Lines about orders, money or offers: their numbers are not one-time codes.
	otherWord = regexp.MustCompile(`(?i)\b(discount|promo\w*|coupon|voucher|gift|orders?|invoice|tracking|postal|zip|referral)\b`)

	// Candidate shapes, tried in this order at each position: digits
	// separated by spaces (from codes laid out one digit per cell),
	// 123-456, Q7X-B2M, 4 to 8 digits, and 5 to 10 upper-case letters and
	// digits.
	candidate = regexp.MustCompile(`\b\d(?: \d){3,7}\b|\b\d{3}[- ]\d{3}\b|\b[A-Z0-9]{3,5}-[A-Z0-9]{3,5}\b|\b\d{4,8}\b|\b[A-Z0-9]{5,10}\b`)

	urlPattern   = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	emailPattern = regexp.MustCompile(`\S+@\S+`)
	year         = regexp.MustCompile(`^(19|20)\d\d$`)
	// "code is" or "code:" right before a candidate.
	namedBefore = regexp.MustCompile(`(?i)\b(codes?|otp|passcode|pin|token|c[oó]digo|codice|kode)\b(\s+is)?\s*:?\s*$`)
)

type found struct {
	code  Code
	score int
	order int
}

func codes(m *mimeparse.Message) []Code {
	lines := []string{m.Subject}
	lines = append(lines, strings.Split(m.PlainText(), "\n")...)
	for i, l := range lines {
		l = urlPattern.ReplaceAllString(l, " ")
		lines[i] = strings.Join(strings.Fields(emailPattern.ReplaceAllString(l, " ")), " ")
	}

	best := map[string]*found{}
	order := 0
	for i, line := range lines {
		if line == "" || otherWord.MatchString(line) {
			continue
		}
		for _, loc := range candidate.FindAllStringIndex(line, -1) {
			raw := line[loc[0]:loc[1]]
			if !plausible(raw) || partOfSomethingElse(line, loc[0], loc[1]) {
				continue
			}
			score, context := scoreCode(lines, i, line, loc[0], loc[1])
			if score < 3 {
				continue
			}
			value := strings.ReplaceAll(raw, " ", "")
			order++
			if f, ok := best[value]; !ok || score > f.score {
				best[value] = &found{code: Code{Value: value, Context: context}, score: score, order: order}
			}
		}
	}

	list := make([]*found, 0, len(best))
	for _, f := range best {
		list = append(list, f)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].score != list[b].score {
			return list[a].score > list[b].score
		}
		return list[a].order < list[b].order
	})
	out := []Code{}
	for _, f := range list {
		if len(out) == maxResults {
			break
		}
		out = append(out, f.code)
	}
	return out
}

// plausible rejects letter-and-digit shapes that are not codes: plain words
// (no digit) and runs of digits matched by the letters-and-digits pattern.
func plausible(s string) bool {
	if strings.ContainsAny(s, " -") || isDigits(s) {
		return true
	}
	hasLetter, hasDigit := false, false
	for _, r := range s {
		hasLetter = hasLetter || unicode.IsLetter(r)
		hasDigit = hasDigit || unicode.IsDigit(r)
	}
	return hasLetter && hasDigit
}

// partOfSomethingElse reports a candidate that belongs to a larger token:
// a price, phone number, date, time, version, order number or host:port.
func partOfSomethingElse(line string, start, end int) bool {
	if start > 0 {
		before := line[start-1]
		if strings.IndexByte("$#+%", before) >= 0 {
			return true
		}
		if strings.IndexByte(".,/-_@", before) >= 0 && start >= 2 && isAlnum(line[start-2]) {
			return true
		}
		// "localhost:3000" but not "Code:482913".
		if before == ':' && start >= 2 && isAlnum(line[start-2]) && !namedBefore.MatchString(line[:start]) {
			return true
		}
		for _, currency := range []string{"€", "£", "¥", "₹"} {
			if strings.HasSuffix(line[:start], currency) {
				return true
			}
		}
	}
	if end < len(line) {
		after := line[end]
		if after == '%' {
			return true
		}
		if strings.IndexByte(".,:/-", after) >= 0 && end+1 < len(line) && isAlnum(line[end+1]) {
			return true
		}
	}
	return false
}

// scoreCode rates a candidate on line i (0 is the subject); 3 or more
// counts as a code. It also returns the sentence to show as context.
func scoreCode(lines []string, i int, line string, start, end int) (int, string) {
	raw := line[start:end]
	alone := strings.Trim(line, " .:!") == raw
	score, context := 0, line

	if words := strongWord.FindAllStringIndex(line, -1); words != nil {
		nearest := len(line)
		for _, w := range words {
			nearest = min(nearest, distance(w, start, end))
		}
		if nearest <= 40 {
			score += 3
		} else {
			score++
		}
	} else if weakWord.MatchString(line) {
		score++
	}

	if alone {
		score++
		// The code shown on a line of its own, below (or above) the
		// sentence that names it.
		for _, j := range neighbours(lines, i) {
			switch {
			case strongWord.MatchString(lines[j]) && !otherWord.MatchString(lines[j]):
				score += 2
				context = lines[j]
			case weakWord.MatchString(lines[j]):
				score++
				if context == line {
					context = lines[j]
				}
			default:
				continue
			}
			break
		}
	}
	if i == 0 && score > 0 {
		score++ // "482913 is your verification code"
	}
	// Years are numbers, not codes, unless the text says so.
	if year.MatchString(raw) && !alone && !namedBefore.MatchString(line[:start]) {
		score -= 3
	}
	return score, truncate(context, 120)
}

// neighbours returns the indexes of the two non-empty lines before line i
// and the one after it, nearest first. The subject is never a neighbour.
func neighbours(lines []string, i int) []int {
	var out []int
	for j, n := i-1, 0; j >= 1 && n < 2; j-- {
		if lines[j] != "" {
			out = append(out, j)
			n++
		}
	}
	for j := i + 1; i > 0 && j < len(lines); j++ {
		if lines[j] != "" {
			out = append(out, j)
			break
		}
	}
	return out
}

func distance(word []int, start, end int) int {
	switch {
	case word[1] <= start:
		return start - word[1]
	case word[0] >= end:
		return word[0] - end
	}
	return 0
}

// ---- Links ----

var (
	// Link kinds, checked in this order against the link text and then the
	// URL. Magic links come first because their URLs often also say
	// "verify" (Supabase: /verify?type=magiclink).
	linkKinds = []struct {
		kind string
		re   *regexp.Regexp
	}{
		{KindLogin, regexp.MustCompile(`(?i)magic|passwordless`)},
		{KindReset, regexp.MustCompile(`(?i)reset|recover|forgot|password`)},
		{KindInvite, regexp.MustCompile(`(?i)invit|join`)},
		{KindVerify, regexp.MustCompile(`(?i)verif|confirm|activat|validat`)},
		{KindLogin, regexp.MustCompile(`(?i)log[-_ ]?in|sign[-_ ]?in|callback|auth|session|otp`)},
	}
	// Footer links that are never the action.
	notAction = regexp.MustCompile(`(?i)unsubscribe|preferences|opt[-_ ]?out|privacy|terms|policy|view (it )?in (your )?browser|view online|web ?version`)
	tokenKey  = regexp.MustCompile(`(?i)^(token|code|key|signature|sig|hash|otp|ticket|nonce|oobcode|.*_ticket)$`)
	textURL   = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+`)
)

type anchor struct{ href, text string }

func links(m *mimeparse.Message) []Link {
	var all []anchor
	if m.HasHTML {
		all = anchors(m.HTML)
	}
	if m.HasText {
		for _, u := range textURL.FindAllString(m.Text, -1) {
			all = append(all, anchor{href: strings.TrimRight(u, ".,;:!?"), text: ""})
		}
	}

	type ranked struct {
		link  Link
		score int
		order int
	}
	var list []ranked
	seen := map[string]bool{}
	for i, a := range all {
		u, err := url.Parse(a.href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[a.href] {
			continue
		}
		if notAction.MatchString(a.text) || notAction.MatchString(u.Path) {
			continue
		}
		where := u.Host + " " + unescape(u.Path) + " " + unescape(u.RawQuery)
		textKind, urlKind := kindOf(a.text), kindOf(where)
		kind := textKind
		if kind == "" {
			kind = urlKind
		}
		token := tokenLike(u)
		// A sign-in or invitation link carries a token; a verify or reset
		// link may instead just say so in its URL.
		if kind == "" || !(token || (urlKind != "" && (kind == KindVerify || kind == KindReset))) {
			continue
		}
		seen[a.href] = true
		score := 0
		if textKind != "" {
			score += 2
		}
		if token {
			score++
		}
		list = append(list, ranked{Link{URL: a.href, Kind: kind, Text: a.text}, score, i})
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].score > list[b].score })

	out := []Link{}
	for _, r := range list {
		if len(out) == maxResults {
			break
		}
		out = append(out, r.link)
	}
	return out
}

func kindOf(s string) string {
	if s == "" {
		return ""
	}
	for _, k := range linkKinds {
		if k.re.MatchString(s) {
			return k.kind
		}
	}
	return ""
}

// tokenLike reports a URL that carries a one-time token: a parameter named
// like one, or a long random-looking path segment or parameter value.
func tokenLike(u *url.URL) bool {
	for key, values := range u.Query() {
		if tokenKey.MatchString(key) {
			return true
		}
		for _, v := range values {
			if randomLooking(v) {
				return true
			}
		}
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if randomLooking(seg) {
			return true
		}
	}
	return false
}

// randomLooking reports a string of 16 or more URL-safe characters that
// mixes digits with letters, as tokens, hashes and signatures do.
func randomLooking(s string) bool {
	if len(s) < 16 {
		return false
	}
	hasDigit, hasLetter := false, false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case unicode.IsLetter(r):
			hasLetter = true
		case strings.ContainsRune("-_.~%=", r):
		default:
			return false
		}
	}
	return hasDigit && hasLetter
}

// anchors returns the HTML part's links with their visible text (or the
// alt text of an image inside the link).
func anchors(s string) []anchor {
	var out []anchor
	z := html.NewTokenizer(strings.NewReader(s))
	var cur *anchor
	var text strings.Builder
	finish := func() {
		if cur != nil {
			cur.text = truncate(strings.Join(strings.Fields(text.String()), " "), 80)
			out = append(out, *cur)
			cur = nil
		}
		text.Reset()
	}
	for {
		switch z.Next() {
		case html.ErrorToken:
			finish()
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.DataAtom {
			case atom.A:
				finish()
				for _, a := range t.Attr {
					if a.Key == "href" {
						cur = &anchor{href: strings.TrimSpace(a.Val)}
					}
				}
			case atom.Img:
				for _, a := range t.Attr {
					if a.Key == "alt" && cur != nil {
						text.WriteString(" " + a.Val + " ")
					}
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

func unescape(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
