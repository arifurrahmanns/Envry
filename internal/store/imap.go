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
)

// Errors returned by folder operations.
var (
	ErrFolderExists    = errors.New("folder already exists")
	ErrFolderNotFound  = errors.New("folder not found")
	ErrFolderProtected = errors.New("INBOX cannot be deleted or renamed")
)

// FolderDelim separates levels in folder names ("Work/2026").
const FolderDelim = "/"

// IMAP system flags as stored on deliveries. Other flags are keywords.
const (
	FlagSeen     = `\Seen`
	FlagAnswered = `\Answered`
	FlagFlagged  = `\Flagged`
	FlagDeleted  = `\Deleted`
	FlagDraft    = `\Draft`
)

// MailFolder is a folder of a mailbox, as IMAP sees it.
type MailFolder struct {
	ID          int64
	Name        string
	SpecialUse  string
	UIDValidity uint32
	UIDNext     uint32
	Subscribed  bool
}

// FolderMessage is one message in a folder, in UID order.
type FolderMessage struct {
	DeliveryID int64
	UID        uint32
	MessageID  string
	Flags      []string
	// Date is when the message arrived in this folder (IMAP INTERNALDATE).
	Date time.Time
	// Size is the size in octets with CRLF line endings.
	Size int64
}

// EnsureMailbox creates an empty mailbox with the default folders if the
// address has none yet, so a client can log in before any mail arrives.
func (s *Store) EnsureMailbox(ctx context.Context, address string) error {
	addr := NormalizeAddress(address)
	var created bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		_, created, err = ensureMailbox(tx, addr, "", s.now().UTC())
		return err
	})
	if err != nil {
		return fmt.Errorf("ensure mailbox: %w", err)
	}
	if created {
		s.publish(events.MailboxCreated, MailboxEvent{Address: addr})
	}
	return nil
}

// canonicalFolderName maps any capitalisation of INBOX to "INBOX".
func canonicalFolderName(name string) string {
	if strings.EqualFold(name, FolderInbox) {
		return FolderInbox
	}
	return name
}

const folderColumns = `f.id, f.name, COALESCE(f.special_use, ''), f.uid_validity, f.uid_next, f.subscribed`

func scanFolder(sc scanner) (MailFolder, error) {
	var f MailFolder
	err := sc.Scan(&f.ID, &f.Name, &f.SpecialUse, &f.UIDValidity, &f.UIDNext, &f.Subscribed)
	return f, err
}

// MailFolders lists a mailbox's folders by name.
func (s *Store) MailFolders(ctx context.Context, address string) ([]MailFolder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+folderColumns+` FROM folders f JOIN mailboxes mb ON mb.id = f.mailbox_id
		WHERE mb.address = ? ORDER BY f.name`, NormalizeAddress(address))
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	var out []MailFolder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MailFolder returns one folder; INBOX matches case-insensitively.
func (s *Store) MailFolder(ctx context.Context, address, name string) (*MailFolder, error) {
	f, err := scanFolder(s.db.QueryRowContext(ctx, `
		SELECT `+folderColumns+` FROM folders f JOIN mailboxes mb ON mb.id = f.mailbox_id
		WHERE mb.address = ? AND f.name = ?`, NormalizeAddress(address), canonicalFolderName(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get folder: %w", err)
	}
	return &f, nil
}

func mailboxID(tx *sql.Tx, address string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM mailboxes WHERE address = ?`, NormalizeAddress(address)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// CreateFolder adds a folder to a mailbox.
func (s *Store) CreateFolder(ctx context.Context, address, name string) error {
	name = canonicalFolderName(strings.TrimRight(name, FolderDelim))
	if name == "" {
		return errors.New("empty folder name")
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		id, err := mailboxID(tx, address)
		if err != nil {
			return err
		}
		// Create missing parents too: "Archive/2026" implies "Archive"
		// (RFC 3501, CREATE).
		parts := strings.Split(name, FolderDelim)
		for i := range parts {
			path := strings.Join(parts[:i+1], FolderDelim)
			res, err := tx.Exec(`INSERT INTO folders (mailbox_id, name, uid_validity) VALUES (?, ?, ?)
				ON CONFLICT (mailbox_id, name) DO NOTHING`, id, path, s.uidValidity())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 && i == len(parts)-1 {
				return ErrFolderExists
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(events.MailboxUpdated, MailboxEvent{Address: NormalizeAddress(address)})
	return nil
}

// DeleteFolder removes a folder and the messages filed in it.
func (s *Store) DeleteFolder(ctx context.Context, address, name string) error {
	name = canonicalFolderName(name)
	if name == FolderInbox {
		return ErrFolderProtected
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		id, err := mailboxID(tx, address)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM folders WHERE mailbox_id = ? AND name = ?`, id, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrFolderNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(events.MailboxUpdated, MailboxEvent{Address: NormalizeAddress(address)})
	return nil
}

// RenameFolder renames a folder and every folder below it in the hierarchy.
func (s *Store) RenameFolder(ctx context.Context, address, oldName, newName string) error {
	oldName = canonicalFolderName(oldName)
	newName = canonicalFolderName(strings.TrimRight(newName, FolderDelim))
	if oldName == FolderInbox || newName == FolderInbox {
		return ErrFolderProtected
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		id, err := mailboxID(tx, address)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM folders WHERE mailbox_id = ? AND name = ?`, id, newName).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return ErrFolderExists
		}
		res, err := tx.Exec(`UPDATE folders SET name = ? WHERE mailbox_id = ? AND name = ?`, newName, id, oldName)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrFolderNotFound
		}
		_, err = tx.Exec(`UPDATE folders SET name = ? || substr(name, ?) WHERE mailbox_id = ? AND substr(name, 1, ?) = ?`,
			newName+FolderDelim, len(oldName)+len(FolderDelim)+1, id, len(oldName)+len(FolderDelim), oldName+FolderDelim)
		return err
	})
	if err != nil {
		return err
	}
	s.publish(events.MailboxUpdated, MailboxEvent{Address: NormalizeAddress(address)})
	return nil
}

// SetFolderSubscribed changes a folder's subscription state.
func (s *Store) SetFolderSubscribed(ctx context.Context, address, name string, subscribed bool) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE folders SET subscribed = ?
		WHERE name = ? AND mailbox_id = (SELECT id FROM mailboxes WHERE address = ?)`,
		subscribed, canonicalFolderName(name), NormalizeAddress(address))
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFolderNotFound
	}
	return nil
}

// uidValidity returns a fresh UIDVALIDITY. Seconds since the epoch, bumped
// past the last value handed out so folders created in the same second
// (or deleted and re-created) never share one.
func (s *Store) uidValidity() uint32 {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	v := uint32(s.now().Unix())
	if v <= s.lastUIDValidity {
		v = s.lastUIDValidity + 1
	}
	s.lastUIDValidity = v
	return v
}

// FolderMessages lists a folder's messages in UID order, including those
// flagged \Deleted but not yet expunged.
func (s *Store) FolderMessages(ctx context.Context, folderID int64) ([]FolderMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.uid, d.message_id, d.seen, d.answered, d.flagged, d.deleted, d.draft, d.keywords,
			d.delivered_at, m.wire_size
		FROM deliveries d JOIN messages m ON m.id = d.message_id
		WHERE d.folder_id = ? ORDER BY d.uid`, folderID)
	if err != nil {
		return nil, fmt.Errorf("list folder messages: %w", err)
	}
	defer rows.Close()
	var out []FolderMessage
	for rows.Next() {
		var (
			m                                       FolderMessage
			seen, answered, flagged, deleted, draft bool
			keywords                                string
			date                                    int64
		)
		if err := rows.Scan(&m.DeliveryID, &m.UID, &m.MessageID, &seen, &answered, &flagged, &deleted, &draft,
			&keywords, &date, &m.Size); err != nil {
			return nil, fmt.Errorf("scan folder message: %w", err)
		}
		m.Date = fromMillis(date)
		m.Flags = flagList(seen, answered, flagged, deleted, draft, keywords)
		out = append(out, m)
	}
	return out, rows.Err()
}

func flagList(seen, answered, flagged, deleted, draft bool, keywords string) []string {
	flags := []string{}
	for _, f := range []struct {
		on   bool
		name string
	}{{seen, FlagSeen}, {answered, FlagAnswered}, {flagged, FlagFlagged}, {deleted, FlagDeleted}, {draft, FlagDraft}} {
		if f.on {
			flags = append(flags, f.name)
		}
	}
	var kw []string
	_ = json.Unmarshal([]byte(keywords), &kw)
	return append(flags, kw...)
}

// flagColumns splits a flag list into the delivery columns. System flags
// match case-insensitively; keywords keep their first spelling.
type flagColumns struct {
	seen, answered, flagged, deleted, draft bool
	keywords                                string
}

func splitFlags(flags []string) flagColumns {
	var c flagColumns
	kw := []string{}
	seenKW := map[string]bool{}
	for _, f := range flags {
		switch strings.ToLower(f) {
		case `\seen`:
			c.seen = true
		case `\answered`:
			c.answered = true
		case `\flagged`:
			c.flagged = true
		case `\deleted`:
			c.deleted = true
		case `\draft`:
			c.draft = true
		case `\recent`, "":
			// \Recent is session state, never stored.
		default:
			if !seenKW[strings.ToLower(f)] {
				seenKW[strings.ToLower(f)] = true
				kw = append(kw, f)
			}
		}
	}
	c.keywords = mustJSON(kw)
	return c
}

// SetDeliveryFlags replaces the full flag set of one delivery.
func (s *Store) SetDeliveryFlags(ctx context.Context, deliveryID int64, flags []string) error {
	c := splitFlags(flags)
	res, err := s.db.ExecContext(ctx, `
		UPDATE deliveries SET seen = ?, answered = ?, flagged = ?, deleted = ?, draft = ?, keywords = ?
		WHERE id = ?`, c.seen, c.answered, c.flagged, c.deleted, c.draft, c.keywords, deliveryID)
	if err != nil {
		return fmt.Errorf("set flags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if d, err := s.GetDelivery(ctx, deliveryID); err == nil {
		s.publish(events.DeliveryUpdated, deliveryEvent(d))
	}
	return nil
}

// CopyDeliveries files copies of deliveries into another folder, keeping
// their flags, and returns the new UIDs in the same order. With move, the
// originals are removed in the same transaction.
func (s *Store) CopyDeliveries(ctx context.Context, deliveryIDs []int64, destFolderID int64, move bool) ([]uint32, error) {
	now := s.now().UTC()
	uids := make([]uint32, 0, len(deliveryIDs))
	var created []int64
	var moved []DeliveryEvent
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range deliveryIDs {
			if move {
				var e DeliveryEvent
				err := tx.QueryRow(`
					SELECT d.id, d.message_id, mb.address, f.name, d.seen, d.flagged
					FROM deliveries d JOIN folders f ON f.id = d.folder_id JOIN mailboxes mb ON mb.id = f.mailbox_id
					WHERE d.id = ?`, id).Scan(&e.DeliveryID, &e.MessageID, &e.Mailbox, &e.Folder, &e.Seen, &e.Flagged)
				if err != nil {
					return fmt.Errorf("load delivery %d: %w", id, err)
				}
				moved = append(moved, e)
			}
			var uid uint32
			if err := tx.QueryRow(`UPDATE folders SET uid_next = uid_next + 1 WHERE id = ? RETURNING uid_next - 1`,
				destFolderID).Scan(&uid); err != nil {
				return fmt.Errorf("allocate uid: %w", err)
			}
			res, err := tx.Exec(`
				INSERT INTO deliveries (message_id, folder_id, uid, rcpt_type, seen, flagged, answered, draft, deleted,
					keywords, delivered_at)
				SELECT message_id, ?, ?, rcpt_type, seen, flagged, answered, draft, deleted, keywords, ?
				FROM deliveries WHERE id = ?`, destFolderID, uid, millis(now), id)
			if err != nil {
				return fmt.Errorf("copy delivery %d: %w", id, err)
			}
			newID, _ := res.LastInsertId()
			created = append(created, newID)
			uids = append(uids, uid)
			if move {
				if _, err := tx.Exec(`DELETE FROM deliveries WHERE id = ?`, id); err != nil {
					return fmt.Errorf("remove moved delivery %d: %w", id, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, e := range moved {
		s.publish(events.DeliveryDeleted, e)
	}
	for _, id := range created {
		if d, err := s.GetDelivery(ctx, id); err == nil {
			s.publish(events.DeliveryCreated, deliveryEvent(d))
		}
	}
	return uids, nil
}

// AppendInput is a message an IMAP client uploads into a folder.
type AppendInput struct {
	Address string
	Folder  string
	Raw     []byte
	Flags   []string
	// Date is the INTERNALDATE; zero means now.
	Date time.Time
}

// AppendMessage stores a message uploaded with IMAP APPEND (a client
// saving to Sent or Drafts) and files it into one folder.
func (s *Store) AppendMessage(ctx context.Context, in AppendInput) (uid, uidValidity uint32, err error) {
	parsed, sum, err := s.prepare(in.Raw, SourceIMAPAppend)
	if err != nil {
		return 0, 0, err
	}
	date := in.Date
	if date.IsZero() {
		date = sum.ReceivedAt
	}
	c := splitFlags(in.Flags)
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		mbID, err := mailboxID(tx, in.Address)
		if err != nil {
			return err
		}
		var folderID int64
		err = tx.QueryRow(`SELECT id, uid_validity FROM folders WHERE mailbox_id = ? AND name = ?`,
			mbID, canonicalFolderName(in.Folder)).Scan(&folderID, &uidValidity)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFolderNotFound
		}
		if err != nil {
			return err
		}
		if err := insertMessage(tx, sum, parsed, IngestInput{Raw: in.Raw, EnvelopeFrom: parsed.From.Address}, nil); err != nil {
			return err
		}
		if err := tx.QueryRow(`UPDATE folders SET uid_next = uid_next + 1 WHERE id = ? RETURNING uid_next - 1`,
			folderID).Scan(&uid); err != nil {
			return fmt.Errorf("allocate uid: %w", err)
		}
		_, err = tx.Exec(`
			INSERT INTO deliveries (message_id, folder_id, uid, rcpt_type, seen, answered, flagged, deleted, draft,
				keywords, delivered_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sum.ID, folderID, uid, RcptSelf, c.seen, c.answered, c.flagged, c.deleted, c.draft, c.keywords, millis(date))
		return err
	})
	if err != nil {
		return 0, 0, fmt.Errorf("append: %w", err)
	}
	s.publish(events.MessageCreated, sum)
	s.prune(ctx)
	return uid, uidValidity, nil
}
