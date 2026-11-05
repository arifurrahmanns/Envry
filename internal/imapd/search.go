package imapd

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"

	"github.com/arifurrahmanns/envry/internal/store"
)

// Search evaluates criteria against the selected folder. Text criteria use
// case-insensitive substring matching on decoded headers and text parts, as
// IMAP requires (word-based full-text search would miss partial words).
func (ss *session) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	sel := ss.sel
	f := sel.folder
	f.mu.Lock()
	defer f.mu.Unlock()
	sel.staticCriteria(criteria)

	var (
		data   imap.SearchData
		seqSet imap.SeqSet
		uidSet imap.UIDSet
	)
	for i, m := range f.msgs {
		seqNum := sel.tracker.EncodeSeqNum(uint32(i + 1))
		mc := &matcher{m: m, seqNum: seqNum, c: &content{st: ss.srv.store, id: m.MessageID}}
		if !mc.match(criteria) {
			continue
		}
		uidSet.AddNum(imap.UID(m.UID))
		var num uint32
		switch kind {
		case imapserver.NumKindSeq:
			if seqNum == 0 {
				continue
			}
			seqSet.AddNum(seqNum)
			num = seqNum
		case imapserver.NumKindUID:
			num = m.UID
		}
		if data.Min == 0 || num < data.Min {
			data.Min = num
		}
		data.Max = max(data.Max, num)
		data.Count++
	}
	if kind == imapserver.NumKindSeq {
		data.All = seqSet
	} else {
		data.All = uidSet
	}
	if options.ReturnSave {
		sel.searchRes = uidSet
	}
	return &data, nil
}

// staticCriteria resolves "*" and "$" in sequence and UID sets, recursively.
func (sel *selection) staticCriteria(c *imap.SearchCriteria) {
	seqs := make([]imap.SeqSet, 0, len(c.SeqNum))
	for _, s := range c.SeqNum {
		switch set := sel.static(s).(type) {
		case imap.SeqSet:
			seqs = append(seqs, set)
		case imap.UIDSet: // "$" saved as UIDs
			c.UID = append(c.UID, set)
		}
	}
	c.SeqNum = seqs
	for i := range c.UID {
		c.UID[i] = sel.static(c.UID[i]).(imap.UIDSet)
	}
	for i := range c.Not {
		sel.staticCriteria(&c.Not[i])
	}
	for i := range c.Or {
		sel.staticCriteria(&c.Or[i][0])
		sel.staticCriteria(&c.Or[i][1])
	}
}

type matcher struct {
	m      store.FolderMessage
	seqNum uint32
	c      *content
}

func (mc *matcher) match(c *imap.SearchCriteria) bool {
	m := mc.m
	for _, set := range c.SeqNum {
		if mc.seqNum == 0 || !set.Contains(mc.seqNum) {
			return false
		}
	}
	for _, set := range c.UID {
		if !set.Contains(imap.UID(m.UID)) {
			return false
		}
	}
	if !inDateRange(m.Date, c.Since, c.Before) {
		return false
	}
	for _, fl := range c.Flag {
		if !hasFlag(m.Flags, fl) {
			return false
		}
	}
	for _, fl := range c.NotFlag {
		if hasFlag(m.Flags, fl) {
			return false
		}
	}
	if c.Larger != 0 && m.Size <= c.Larger {
		return false
	}
	if c.Smaller != 0 && m.Size >= c.Smaller {
		return false
	}

	if len(c.Header) > 0 || !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
		h, ok := mc.header()
		if !ok {
			return false
		}
		for _, field := range c.Header {
			if !headerContains(h.FieldsByKey(field.Key), field.Value) {
				return false
			}
		}
		if !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
			sent, err := h.Date()
			if err != nil || !inDateRange(sent, c.SentSince, c.SentBefore) {
				return false
			}
		}
	}
	for _, text := range c.Text {
		if !mc.contains(text, true) {
			return false
		}
	}
	for _, text := range c.Body {
		if !mc.contains(text, false) {
			return false
		}
	}

	for i := range c.Not {
		if mc.match(&c.Not[i]) {
			return false
		}
	}
	for i := range c.Or {
		if !mc.match(&c.Or[i][0]) && !mc.match(&c.Or[i][1]) {
			return false
		}
	}
	return true
}

// inDateRange compares dates only, ignoring time and zone (RFC 3501).
func inDateRange(t, since, before time.Time) bool {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if !since.IsZero() && day.Before(since) {
		return false
	}
	if !before.IsZero() && !day.Before(before) {
		return false
	}
	return true
}

func (mc *matcher) entity() (*message.Entity, bool) {
	raw, err := mc.c.bytes()
	if err != nil {
		return nil, false
	}
	e, err := message.Read(bytes.NewReader(raw))
	if e == nil || (err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err)) {
		return nil, false
	}
	return e, true
}

func (mc *matcher) header() (mail.Header, bool) {
	e, ok := mc.entity()
	if !ok {
		return mail.Header{}, false
	}
	return mail.Header{Header: e.Header}, true
}

func headerContains(fields message.HeaderFields, value string) bool {
	if value == "" {
		return fields.Len() > 0
	}
	value = strings.ToLower(value)
	for fields.Next() {
		v, err := fields.Text()
		if err != nil {
			v = fields.Value()
		}
		if strings.Contains(strings.ToLower(v), value) {
			return true
		}
	}
	return false
}

// contains looks for text in the decoded text parts, and in the headers
// too for SEARCH TEXT.
func (mc *matcher) contains(text string, includeHeaders bool) bool {
	if text == "" {
		return true
	}
	e, ok := mc.entity()
	if !ok {
		return false
	}
	needle := []byte(strings.ToLower(text))
	if includeHeaders && headerContains(e.Header.Fields(), text) {
		return true
	}
	found := false
	_ = e.Walk(func(_ []int, part *message.Entity, err error) error {
		if found || (err != nil && !message.IsUnknownCharset(err)) {
			return nil
		}
		mediaType, _, _ := part.Header.ContentType()
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if !strings.HasPrefix(mediaType, "text/") {
			return nil
		}
		body, _ := io.ReadAll(io.LimitReader(part.Body, 32<<20))
		found = bytes.Contains(bytes.ToLower(body), needle)
		return nil
	})
	return found
}
