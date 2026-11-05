package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// IngestInput is one message to store.
type IngestInput struct {
	EnvelopeFrom string
	// Recipients are the envelope recipients (RCPT TO). Each gets a
	// delivery in its mailbox's INBOX.
	Recipients []string
	Raw        []byte
	Source     string
	ClientIP   string
	// AuthUser is the SMTP AUTH username. When it names an Inbox account,
	// the message is also filed in that account's Sent folder.
	AuthUser string
}

// Ingest parses and stores a message, delivers it to every recipient's
// mailbox (creating mailboxes as needed), publishes message.created and
// applies the retention limit.
func (s *Store) Ingest(ctx context.Context, in IngestInput) (*MessageSummary, error) {
	parsed, err := mimeparse.Parse(in.Raw)
	if err != nil {
		s.log.Warn("storing message with unreadable headers", "err", err)
		parsed = &mimeparse.Message{}
	}
	if in.Source == "" {
		in.Source = SourceSMTP
	}

	now := s.now().UTC()
	id := s.newID(now)
	rcpts := classifyRecipients(in.Recipients, parsed)
	sum := summarize(id, parsed, int64(len(in.Raw)), in.Source, now)

	var newMailboxes []string
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := insertMessage(tx, sum, parsed, in, rcpts); err != nil {
			return err
		}
		for _, r := range rcpts {
			created, err := deliver(tx, deliverArgs{
				messageID:   id,
				address:     r.Address,
				displayName: nameFor(r.Address, parsed),
				folder:      FolderInbox,
				rcptType:    r.Type,
				now:         now,
			})
			if err != nil {
				return err
			}
			if created {
				newMailboxes = append(newMailboxes, r.Address)
			}
		}
		if user := NormalizeAddress(in.AuthUser); user != "" {
			var isAccount bool
			err := tx.QueryRow(`SELECT is_account FROM mailboxes WHERE address = ?`, user).Scan(&isAccount)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("look up sender account: %w", err)
			}
			if isAccount {
				if _, err := deliver(tx, deliverArgs{
					messageID: id, address: user, folder: FolderSent,
					rcptType: RcptSelf, seen: true, now: now,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	for _, a := range newMailboxes {
		s.publish(events.MailboxCreated, MailboxEvent{Address: a})
	}
	s.publish(events.MessageCreated, sum)
	s.prune(ctx)
	return &sum, nil
}

func summarize(id string, p *mimeparse.Message, size int64, source string, now time.Time) MessageSummary {
	sum := MessageSummary{
		ID:              id,
		From:            p.From,
		To:              nonNil(p.To),
		Cc:              nonNil(p.Cc),
		Subject:         p.Subject,
		Preview:         p.Preview(),
		ReceivedAt:      now,
		Size:            size,
		HasHTML:         p.HasHTML,
		HasText:         p.HasText,
		AttachmentCount: len(p.Attachments),
		Source:          source,
	}
	if !p.Date.IsZero() {
		d := p.Date.UTC()
		sum.Date = &d
	}
	return sum
}

func insertMessage(tx *sql.Tx, sum MessageSummary, p *mimeparse.Message, in IngestInput, rcpts []Recipient) error {
	var date sql.NullInt64
	if sum.Date != nil {
		date = sql.NullInt64{Int64: millis(*sum.Date), Valid: true}
	}
	_, err := tx.Exec(`
		INSERT INTO messages (id, message_id_hdr, envelope_from, recipients, from_hdr, to_hdr, cc_hdr,
			reply_to_hdr, subject, date_hdr, preview, size, has_html, has_text, attachment_count,
			source, client_ip, auth_user, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sum.ID, toNull(p.MessageID), NormalizeAddress(in.EnvelopeFrom), mustJSON(nonNilRcpts(rcpts)),
		mustJSON(sum.From), mustJSON(sum.To), mustJSON(sum.Cc), mustJSON(nonNil(p.ReplyTo)),
		sum.Subject, date, sum.Preview, sum.Size, sum.HasHTML, sum.HasText, sum.AttachmentCount,
		sum.Source, toNull(in.ClientIP), toNull(in.AuthUser), millis(sum.ReceivedAt),
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO message_raw (message_id, raw) VALUES (?, ?)`, sum.ID, in.Raw); err != nil {
		return fmt.Errorf("insert raw: %w", err)
	}
	for i, a := range p.Attachments {
		_, err := tx.Exec(`
			INSERT INTO attachments (message_id, id, filename, content_type, content_id, inline, size, part_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sum.ID, strconv.Itoa(i+1), a.Filename, a.ContentType, toNull(a.ContentID), a.Inline, a.Size, a.PartPath)
		if err != nil {
			return fmt.Errorf("insert attachment: %w", err)
		}
	}

	var to []string
	for _, a := range append(append([]Address{}, sum.To...), sum.Cc...) {
		to = append(to, a.Name, a.Address)
	}
	for _, r := range rcpts {
		to = append(to, r.Address)
	}
	_, err = tx.Exec(`INSERT INTO messages_fts (message_id, subject, from_hdr, to_hdr, body) VALUES (?, ?, ?, ?, ?)`,
		sum.ID, sum.Subject, strings.TrimSpace(sum.From.Name+" "+sum.From.Address), strings.Join(to, " "), p.PlainText())
	if err != nil {
		return fmt.Errorf("index message: %w", err)
	}
	return nil
}

type deliverArgs struct {
	messageID   string
	address     string
	displayName string
	folder      string
	rcptType    string
	seen        bool
	now         time.Time
}

// deliver files a message into a mailbox folder, creating the mailbox (with
// its default folders) on first use. It reports whether the mailbox is new.
func deliver(tx *sql.Tx, a deliverArgs) (bool, error) {
	mailboxID, created, err := ensureMailbox(tx, a.address, a.displayName, a.now)
	if err != nil {
		return false, err
	}
	var folderID int64
	if err := tx.QueryRow(`SELECT id FROM folders WHERE mailbox_id = ? AND name = ?`, mailboxID, a.folder).
		Scan(&folderID); err != nil {
		return false, fmt.Errorf("find folder %s for %s: %w", a.folder, a.address, err)
	}
	var uid int64
	if err := tx.QueryRow(`UPDATE folders SET uid_next = uid_next + 1 WHERE id = ? RETURNING uid_next - 1`, folderID).
		Scan(&uid); err != nil {
		return false, fmt.Errorf("allocate uid: %w", err)
	}
	_, err = tx.Exec(`
		INSERT INTO deliveries (message_id, folder_id, uid, rcpt_type, seen, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?)`, a.messageID, folderID, uid, a.rcptType, a.seen, millis(a.now))
	if err != nil {
		return false, fmt.Errorf("insert delivery: %w", err)
	}
	return created, nil
}

var defaultFolders = []struct{ name, specialUse string }{
	{FolderInbox, ""},
	{FolderSent, `\Sent`},
	{FolderDrafts, `\Drafts`},
	{FolderTrash, `\Trash`},
}

func ensureMailbox(tx *sql.Tx, address, displayName string, now time.Time) (int64, bool, error) {
	res, err := tx.Exec(`
		INSERT INTO mailboxes (address, display_name, created_at) VALUES (?, ?, ?)
		ON CONFLICT (address) DO NOTHING`, address, toNull(displayName), millis(now))
	if err != nil {
		return 0, false, fmt.Errorf("create mailbox: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		id, err := res.LastInsertId()
		if err != nil {
			return 0, false, err
		}
		for _, f := range defaultFolders {
			_, err := tx.Exec(`INSERT INTO folders (mailbox_id, name, special_use, uid_validity) VALUES (?, ?, ?, ?)`,
				id, f.name, toNull(f.specialUse), now.Unix())
			if err != nil {
				return 0, false, fmt.Errorf("create folder: %w", err)
			}
		}
		return id, true, nil
	}

	var id int64
	if err := tx.QueryRow(`SELECT id FROM mailboxes WHERE address = ?`, address).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("find mailbox: %w", err)
	}
	if displayName != "" {
		if _, err := tx.Exec(`UPDATE mailboxes SET display_name = ? WHERE id = ? AND display_name IS NULL`,
			displayName, id); err != nil {
			return 0, false, fmt.Errorf("set display name: %w", err)
		}
	}
	return id, false, nil
}

// classifyRecipients de-duplicates envelope recipients and labels each by
// where it appears in the headers: To, Cc, or neither (Bcc).
func classifyRecipients(envelope []string, p *mimeparse.Message) []Recipient {
	inTo, inCc := map[string]bool{}, map[string]bool{}
	for _, a := range p.To {
		inTo[a.Address] = true
	}
	for _, a := range p.Cc {
		inCc[a.Address] = true
	}
	seen := map[string]bool{}
	var out []Recipient
	for _, r := range envelope {
		a := NormalizeAddress(r)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		typ := RcptBcc
		switch {
		case inTo[a]:
			typ = RcptTo
		case inCc[a]:
			typ = RcptCc
		}
		out = append(out, Recipient{Address: a, Type: typ})
	}
	return out
}

func nameFor(address string, p *mimeparse.Message) string {
	for _, a := range append(append([]mimeparse.Address{}, p.To...), p.Cc...) {
		if a.Address == address && a.Name != "" {
			return a.Name
		}
	}
	return ""
}

// prune deletes the oldest messages beyond the retention limit.
func (s *Store) prune(ctx context.Context) {
	if s.maxMessages <= 0 {
		return
	}
	var ids []string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id FROM messages ORDER BY id DESC LIMIT -1 OFFSET ?`, s.maxMessages)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.Exec(`DELETE FROM messages WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("retention prune failed", "err", err)
		return
	}
	for _, id := range ids {
		s.publish(events.MessageDeleted, MessageRef{ID: id})
	}
	if len(ids) > 0 {
		s.log.Debug("pruned old messages", "count", len(ids), "keep", s.maxMessages)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only plain structs and slices are encoded
	}
	return string(b)
}

func nonNil(a []Address) []Address {
	if a == nil {
		return []Address{}
	}
	return a
}

func nonNilRcpts(r []Recipient) []Recipient {
	if r == nil {
		return []Recipient{}
	}
	return r
}
