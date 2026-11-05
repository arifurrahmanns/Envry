package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// ListOptions filters and paginates a message list.
type ListOptions struct {
	// Query is free text matched against subject, addresses and body.
	Query string
	// From matches the sender address exactly (header or envelope).
	From string
	// To matches an envelope recipient exactly.
	To string
	// Before is a cursor: only messages older than this message ID.
	Before string
	Limit  int
}

func (o ListOptions) limit() int {
	switch {
	case o.Limit <= 0:
		return DefaultPageSize
	case o.Limit > MaxPageSize:
		return MaxPageSize
	}
	return o.Limit
}

// where collects SQL conditions and their arguments.
type where struct {
	conds []string
	args  []any
}

func (w *where) add(cond string, args ...any) {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
}

func (w *where) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

// messageFilters adds the search and address filters shared by every list.
func messageFilters(w *where, o ListOptions) {
	if q := ftsQuery(o.Query); q != "" {
		w.add(`m.id IN (SELECT message_id FROM messages_fts WHERE messages_fts MATCH ?)`, q)
	}
	if from := NormalizeAddress(o.From); from != "" {
		w.add(`(m.envelope_from = ? OR json_extract(m.from_hdr, '$.address') = ?)`, from, from)
	}
	if to := NormalizeAddress(o.To); to != "" {
		w.add(`EXISTS (SELECT 1 FROM json_each(m.recipients) r WHERE json_extract(r.value, '$.address') = ?)`, to)
	}
}

const summaryColumns = `m.id, m.from_hdr, m.to_hdr, m.cc_hdr, m.subject, m.preview, m.date_hdr,
	m.received_at, m.size, m.has_html, m.has_text, m.attachment_count, m.source`

type scanner interface {
	Scan(dest ...any) error
}

// scanSummary scans summaryColumns followed by any extra destinations.
func scanSummary(sc scanner, extra ...any) (MessageSummary, error) {
	var (
		m             MessageSummary
		from, to, cc  string
		date          sql.NullInt64
		receivedAtSQL int64
	)
	dest := append([]any{&m.ID, &from, &to, &cc, &m.Subject, &m.Preview, &date, &receivedAtSQL,
		&m.Size, &m.HasHTML, &m.HasText, &m.AttachmentCount, &m.Source}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return m, err
	}
	m.Date = nullMillis(date)
	m.ReceivedAt = fromMillis(receivedAtSQL)
	_ = json.Unmarshal([]byte(from), &m.From)
	_ = json.Unmarshal([]byte(to), &m.To)
	_ = json.Unmarshal([]byte(cc), &m.Cc)
	m.To, m.Cc = nonNil(m.To), nonNil(m.Cc)
	return m, nil
}

// ListMessages lists every captured message, newest first.
func (s *Store) ListMessages(ctx context.Context, o ListOptions) (Page[MessageSummary], error) {
	page := Page[MessageSummary]{Items: []MessageSummary{}}
	var w where
	messageFilters(&w, o)

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages m`+w.sql(), w.args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count messages: %w", err)
	}
	if o.Before != "" {
		w.add(`m.id < ?`, o.Before)
	}
	limit := o.limit()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+summaryColumns+` FROM messages m`+w.sql()+` ORDER BY m.id DESC LIMIT ?`,
		append(w.args, limit+1)...)
	if err != nil {
		return page, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanSummary(rows)
		if err != nil {
			return page, fmt.Errorf("scan message: %w", err)
		}
		page.Items = append(page.Items, m)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("list messages: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		next := page.Items[limit-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

// GetMessage returns a message with headers, text body and attachments.
func (s *Store) GetMessage(ctx context.Context, id string) (*MessageDetail, error) {
	var (
		d                         MessageDetail
		msgID, clientIP, authUser sql.NullString
		recipients, replyTo       string
		raw                       []byte
	)
	row := s.db.QueryRowContext(ctx, `
		SELECT `+summaryColumns+`, m.message_id_hdr, m.envelope_from, m.recipients, m.reply_to_hdr,
			m.client_ip, m.auth_user, r.raw
		FROM messages m JOIN message_raw r ON r.message_id = m.id
		WHERE m.id = ?`, id)
	sum, err := scanSummary(row, &msgID, &d.EnvelopeFrom, &recipients, &replyTo, &clientIP, &authUser, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	d.MessageSummary = sum
	d.MessageID, d.ClientIP, d.AuthUser = nullString(msgID), nullString(clientIP), nullString(authUser)
	_ = json.Unmarshal([]byte(recipients), &d.Recipients)
	_ = json.Unmarshal([]byte(replyTo), &d.ReplyTo)
	d.ReplyTo = nonNil(d.ReplyTo)
	d.Recipients = nonNilRcpts(d.Recipients)

	d.Headers = []Header{}
	if p, err := mimeparse.Parse(raw); err == nil {
		if p.Headers != nil {
			d.Headers = p.Headers
		}
		if p.HasText {
			d.Text = &p.Text
		}
	}

	d.Attachments, err = s.attachments(ctx, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) attachments(ctx context.Context, messageID string) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, filename, content_type, content_id, inline, size, part_path
		FROM attachments WHERE message_id = ? ORDER BY CAST(id AS INTEGER)`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		var cid sql.NullString
		if err := rows.Scan(&a.ID, &a.Filename, &a.ContentType, &cid, &a.Inline, &a.Size, &a.PartPath); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		a.ContentID = nullString(cid)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Raw returns the exact bytes received for a message.
func (s *Store) Raw(ctx context.Context, id string) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT raw FROM message_raw WHERE message_id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get raw message: %w", err)
	}
	return raw, nil
}

// AttachmentContent returns an attachment's metadata and decoded bytes.
func (s *Store) AttachmentContent(ctx context.Context, messageID, attachmentID string) (*Attachment, []byte, error) {
	list, err := s.attachments(ctx, messageID)
	if err != nil {
		return nil, nil, err
	}
	for _, a := range list {
		if a.ID != attachmentID {
			continue
		}
		raw, err := s.Raw(ctx, messageID)
		if err != nil {
			return nil, nil, err
		}
		_, body, err := mimeparse.ExtractPart(raw, a.PartPath)
		if errors.Is(err, mimeparse.ErrPartNotFound) {
			return nil, nil, ErrNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return &a, body, nil
	}
	return nil, nil, ErrNotFound
}

// DeleteMessage removes a message from Sent and from every mailbox.
func (s *Store) DeleteMessage(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.publish(events.MessageDeleted, MessageRef{ID: id})
	return nil
}

// DeleteAllMessages removes every message, plus the mailboxes that only
// existed because something was sent to them. Accounts are kept.
func (s *Store) DeleteAllMessages(ctx context.Context) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM messages`,
			`DELETE FROM messages_fts`,
			`DELETE FROM mailboxes WHERE is_account = 0`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete all messages: %w", err)
	}
	s.publish(events.MessagesCleared, nil)
	return nil
}

// WaitFilter selects the message /wait returns.
type WaitFilter struct {
	To      string
	From    string
	Subject string // case-insensitive substring
	Since   time.Time
}

// LatestMatching returns the newest message matching f, or ErrNotFound.
func (s *Store) LatestMatching(ctx context.Context, f WaitFilter) (*MessageSummary, error) {
	var w where
	messageFilters(&w, ListOptions{From: f.From, To: f.To})
	if f.Subject != "" {
		w.add(`m.subject LIKE ? ESCAPE '\'`, likeContains(f.Subject))
	}
	if !f.Since.IsZero() {
		w.add(`m.received_at >= ?`, millis(f.Since))
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+summaryColumns+` FROM messages m`+w.sql()+` ORDER BY m.id DESC LIMIT 1`, w.args...)
	m, err := scanSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find message: %w", err)
	}
	return &m, nil
}

// Stats are totals for /api/v1/info.
type Stats struct {
	Messages  int `json:"messages"`
	Mailboxes int `json:"mailboxes"`
	Accounts  int `json:"accounts"`
}

// Stats counts messages, mailboxes and accounts.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM messages),
		       (SELECT COUNT(*) FROM mailboxes),
		       (SELECT COUNT(*) FROM mailboxes WHERE is_account = 1)`).
		Scan(&st.Messages, &st.Mailboxes, &st.Accounts)
	if err != nil {
		return st, fmt.Errorf("stats: %w", err)
	}
	return st, nil
}
