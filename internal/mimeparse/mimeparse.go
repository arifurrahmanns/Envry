// Package mimeparse turns a raw RFC 5322 message into the structured parts
// Envry stores and shows: addresses, subject, text and HTML bodies, and
// attachments addressed by IMAP-style part paths ("2", "2.1").
package mimeparse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // decode non-UTF-8 bodies and headers
	gomail "github.com/emersion/go-message/mail"
)

// maxPartBytes caps how much of a single part is read into memory.
const maxPartBytes = 64 << 20

// Address is a parsed mailbox. Address is lower-cased when it looks valid.
type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Header is one header field with its value decoded to UTF-8.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Attachment describes a non-body part.
type Attachment struct {
	Filename    string
	ContentType string
	ContentID   string // without angle brackets
	Inline      bool
	Size        int
	PartPath    string
}

// Message is the parsed form of a raw message.
type Message struct {
	Headers   []Header
	MessageID string
	Subject   string
	Date      time.Time // zero when missing or unparsable
	From      Address
	To        []Address
	Cc        []Address
	ReplyTo   []Address

	Text    string
	HTML    string
	HasText bool
	HasHTML bool

	Attachments []Attachment
}

// Parse reads raw and never fails on malformed input: unknown charsets,
// broken encodings and bad address lists degrade to their raw form.
// It only returns an error when the header block itself cannot be read.
func Parse(raw []byte) (*Message, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !isRecoverable(err) {
		return nil, fmt.Errorf("read message: %w", err)
	}

	h := gomail.Header{Header: entity.Header}
	m := &Message{
		Headers: headers(entity.Header),
		From:    firstAddress(h, "From"),
		To:      addressList(h, "To"),
		Cc:      addressList(h, "Cc"),
		ReplyTo: addressList(h, "Reply-To"),
	}
	m.Subject, _ = h.Subject()
	m.MessageID, _ = h.MessageID()
	if d, err := h.Date(); err == nil {
		m.Date = d
	}

	err = entity.Walk(func(path []int, e *message.Entity, walkErr error) error {
		if walkErr != nil && !isRecoverable(walkErr) {
			return nil // skip the broken part, keep the rest
		}
		mediaType, params, _ := e.Header.ContentType()
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			return nil
		}
		body, _ := io.ReadAll(io.LimitReader(e.Body, maxPartBytes))
		m.addPart(partPath(path), mediaType, params, e.Header, body)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk message: %w", err)
	}
	return m, nil
}

func (m *Message) addPart(path, mediaType string, params map[string]string, h message.Header, body []byte) {
	disp, dispParams, _ := h.ContentDisposition()
	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}
	contentID := strings.Trim(strings.TrimSpace(h.Get("Content-Id")), "<>")

	isBody := disp != "attachment" && filename == "" &&
		(mediaType == "text/plain" || mediaType == "text/html")
	if isBody {
		switch {
		case mediaType == "text/plain" && !m.HasText:
			m.Text, m.HasText = string(body), true
			return
		case mediaType == "text/html" && !m.HasHTML:
			m.HTML, m.HasHTML = string(body), true
			return
		}
	}

	m.Attachments = append(m.Attachments, Attachment{
		Filename:    filename,
		ContentType: mediaType,
		ContentID:   contentID,
		Inline:      disp == "inline" || (disp == "" && contentID != ""),
		Size:        len(body),
		PartPath:    path,
	})
}

// ExtractPart returns the decoded body of the part at path (as recorded in
// Attachment.PartPath) and its media type.
func ExtractPart(raw []byte, path string) (mediaType string, body []byte, err error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !isRecoverable(err) {
		return "", nil, fmt.Errorf("read message: %w", err)
	}
	found := false
	err = entity.Walk(func(p []int, e *message.Entity, walkErr error) error {
		if found || partPath(p) != path {
			return nil
		}
		if walkErr != nil && !isRecoverable(walkErr) {
			return walkErr
		}
		mediaType, _, _ = e.Header.ContentType()
		if strings.HasPrefix(mediaType, "multipart/") {
			return nil
		}
		body, err = io.ReadAll(io.LimitReader(e.Body, maxPartBytes))
		found = true
		return err
	})
	if err != nil {
		return "", nil, fmt.Errorf("extract part %q: %w", path, err)
	}
	if !found {
		return "", nil, ErrPartNotFound
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return mediaType, body, nil
}

// ErrPartNotFound is returned by ExtractPart for an unknown path.
var ErrPartNotFound = errors.New("part not found")

// partPath converts go-message's 0-based walk path to an IMAP-style 1-based
// path. The root of a non-multipart message is "1".
func partPath(path []int) string {
	if len(path) == 0 {
		return "1"
	}
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = strconv.Itoa(n + 1)
	}
	return strings.Join(parts, ".")
}

func isRecoverable(err error) bool {
	return message.IsUnknownCharset(err) || message.IsUnknownEncoding(err)
}

func headers(h message.Header) []Header {
	var out []Header
	fields := h.Fields()
	for fields.Next() {
		v, err := fields.Text()
		if err != nil {
			v = fields.Value()
		}
		out = append(out, Header{Name: fields.Key(), Value: v})
	}
	return out
}

func firstAddress(h gomail.Header, key string) Address {
	if list := addressList(h, key); len(list) > 0 {
		return list[0]
	}
	return Address{}
}

// addressList parses an address header, falling back to a lenient split
// when the header is not RFC 5322 compliant.
func addressList(h gomail.Header, key string) []Address {
	if h.Get(key) == "" {
		return nil
	}
	if list, err := h.AddressList(key); err == nil {
		out := make([]Address, 0, len(list))
		for _, a := range list {
			out = append(out, Address{Name: a.Name, Address: strings.ToLower(a.Address)})
		}
		return out
	}
	raw, err := h.Text(key)
	if err != nil {
		raw = h.Get(key)
	}
	return ParseAddressList(raw)
}

// ParseAddressList parses a comma-separated address list, keeping whatever
// it can from entries that are not valid RFC 5322 addresses.
func ParseAddressList(s string) []Address {
	if list, err := mail.ParseAddressList(s); err == nil {
		out := make([]Address, 0, len(list))
		for _, a := range list {
			out = append(out, Address{Name: a.Name, Address: strings.ToLower(a.Address)})
		}
		return out
	}
	var out []Address
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, err := mail.ParseAddress(part); err == nil {
			out = append(out, Address{Name: a.Name, Address: strings.ToLower(a.Address)})
			continue
		}
		name, addr := "", part
		if i, j := strings.LastIndexByte(part, '<'), strings.LastIndexByte(part, '>'); i >= 0 && j > i {
			name = strings.Trim(strings.TrimSpace(part[:i]), `"`)
			addr = part[i+1 : j]
		}
		out = append(out, Address{Name: name, Address: strings.ToLower(strings.TrimSpace(addr))})
	}
	return out
}
