package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"

	"github.com/arifurrahmanns/envry/internal/events"
)

// ListMailboxes returns every mailbox that has mail or is an account, with
// per-folder counts.
func (s *Store) ListMailboxes(ctx context.Context) ([]Mailbox, error) {
	return s.mailboxes(ctx, "")
}

// GetMailbox returns one mailbox by address.
func (s *Store) GetMailbox(ctx context.Context, address string) (*Mailbox, error) {
	list, err := s.mailboxes(ctx, NormalizeAddress(address))
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

func (s *Store) mailboxes(ctx context.Context, address string) ([]Mailbox, error) {
	query := `
		SELECT mb.address, mb.display_name, mb.is_account, f.name, f.special_use,
			COUNT(d.id), COALESCE(SUM(d.seen = 0), 0), MAX(d.delivered_at)
		FROM mailboxes mb
		JOIN folders f ON f.mailbox_id = mb.id
		LEFT JOIN deliveries d ON d.folder_id = f.id AND d.deleted = 0`
	var args []any
	if address != "" {
		query += ` WHERE mb.address = ?`
		args = append(args, address)
	}
	query += ` GROUP BY f.id ORDER BY mb.address, f.id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}
	defer rows.Close()

	var out []Mailbox
	for rows.Next() {
		var (
			addr, folder         string
			displayName, special sql.NullString
			isAccount            bool
			total, unread        int
			last                 sql.NullInt64
		)
		if err := rows.Scan(&addr, &displayName, &isAccount, &folder, &special, &total, &unread, &last); err != nil {
			return nil, fmt.Errorf("scan mailbox: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].Address != addr {
			out = append(out, Mailbox{
				Address:     addr,
				DisplayName: nullString(displayName),
				IsAccount:   isAccount,
				Folders:     []Folder{},
			})
		}
		mb := &out[len(out)-1]
		mb.Folders = append(mb.Folders, Folder{Name: folder, SpecialUse: nullString(special), Total: total, Unread: unread})
		mb.Total += total
		mb.Unread += unread
		if t := nullMillis(last); t != nil && (mb.LastReceivedAt == nil || t.After(*mb.LastReceivedAt)) {
			mb.LastReceivedAt = t
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}

	// Mailboxes emptied by deletes stay in the database for IMAP UID
	// stability, but only accounts and mailboxes with mail are listed.
	kept := []Mailbox{}
	for _, mb := range out {
		if mb.IsAccount || mb.Total > 0 || address != "" {
			kept = append(kept, mb)
		}
	}
	return kept, nil
}

const deliveryColumns = summaryColumns + `, d.id, mb.address, f.name, d.rcpt_type, d.seen, d.flagged`

const deliveryJoins = ` FROM deliveries d
	JOIN folders f ON f.id = d.folder_id
	JOIN mailboxes mb ON mb.id = f.mailbox_id
	JOIN messages m ON m.id = d.message_id`

func scanDelivery(sc scanner) (Delivery, error) {
	var d Delivery
	sum, err := scanSummary(sc, &d.DeliveryID, &d.Mailbox, &d.Folder, &d.RcptType, &d.Seen, &d.Flagged)
	d.MessageSummary = sum
	return d, err
}

// MailboxMessages lists one folder of a mailbox, newest first.
func (s *Store) MailboxMessages(ctx context.Context, address, folder string, o ListOptions) (Page[Delivery], error) {
	page := Page[Delivery]{Items: []Delivery{}}
	if folder == "" {
		folder = FolderInbox
	}
	var w where
	w.add(`mb.address = ?`, NormalizeAddress(address))
	w.add(`f.name = ?`, folder)
	w.add(`d.deleted = 0`)
	messageFilters(&w, ListOptions{Query: o.Query, From: o.From})

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+deliveryJoins+w.sql(), w.args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count deliveries: %w", err)
	}
	if o.Before != "" {
		w.add(`m.id < ?`, o.Before)
	}
	limit := o.limit()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deliveryColumns+deliveryJoins+w.sql()+` ORDER BY m.id DESC, d.id DESC LIMIT ?`,
		append(w.args, limit+1)...)
	if err != nil {
		return page, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return page, fmt.Errorf("scan delivery: %w", err)
		}
		page.Items = append(page.Items, d)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("list deliveries: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		next := page.Items[limit-1].ID
		page.NextCursor = &next
	}
	return page, nil
}

// GetDelivery returns one delivery.
func (s *Store) GetDelivery(ctx context.Context, id int64) (*Delivery, error) {
	d, err := scanDelivery(s.db.QueryRowContext(ctx, `SELECT `+deliveryColumns+deliveryJoins+` WHERE d.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get delivery: %w", err)
	}
	return &d, nil
}

// DeliveryPatch changes flags on a delivery; nil fields are left alone.
type DeliveryPatch struct {
	Seen    *bool `json:"seen"`
	Flagged *bool `json:"flagged"`
}

// UpdateDelivery sets flags on one mailbox's copy of a message.
func (s *Store) UpdateDelivery(ctx context.Context, id int64, p DeliveryPatch) (*Delivery, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE deliveries SET seen = COALESCE(?, seen), flagged = COALESCE(?, flagged) WHERE id = ?`,
		nullBool(p.Seen), nullBool(p.Flagged), id)
	if err != nil {
		return nil, fmt.Errorf("update delivery: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	d, err := s.GetDelivery(ctx, id)
	if err != nil {
		return nil, err
	}
	s.publish(events.DeliveryUpdated, deliveryEvent(d))
	return d, nil
}

// DeleteDelivery removes one mailbox's copy of a message. The message stays
// in Sent and in the other recipients' mailboxes.
func (s *Store) DeleteDelivery(ctx context.Context, id int64) error {
	d, err := s.GetDelivery(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM deliveries WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete delivery: %w", err)
	}
	s.publish(events.DeliveryDeleted, deliveryEvent(d))
	return nil
}

func deliveryEvent(d *Delivery) DeliveryEvent {
	return DeliveryEvent{
		DeliveryID: d.DeliveryID, MessageID: d.ID, Mailbox: d.Mailbox,
		Folder: d.Folder, Seen: d.Seen, Flagged: d.Flagged,
	}
}

func nullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

// AccountInput creates or updates an Inbox account.
type AccountInput struct {
	Address     string
	DisplayName string
	// Password is checked at login only with strict auth. Empty keeps the
	// current password (none for a new account, which accepts any).
	Password string
}

// EnsureAccount marks a mailbox as an Inbox account, creating it if needed.
// It reports whether the mailbox is new.
func (s *Store) EnsureAccount(ctx context.Context, in AccountInput) (*Mailbox, bool, error) {
	addr := NormalizeAddress(in.Address)
	if addr == "" {
		return nil, false, errors.New("account address is empty")
	}
	var created bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		_, isNew, err := ensureMailbox(tx, addr, in.DisplayName, s.now().UTC())
		if err != nil {
			return err
		}
		created = isNew
		_, err = tx.Exec(`
			UPDATE mailboxes SET is_account = 1, password = COALESCE(?, password),
				display_name = COALESCE(?, display_name)
			WHERE address = ?`, toNull(in.Password), toNull(in.DisplayName), addr)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("ensure account: %w", err)
	}
	if created {
		s.publish(events.MailboxCreated, MailboxEvent{Address: addr})
	} else {
		s.publish(events.MailboxUpdated, MailboxEvent{Address: addr})
	}
	mb, err := s.GetMailbox(ctx, addr)
	return mb, created, err
}

// RemoveAccount turns an account back into a plain recipient mailbox. Its
// mail is kept.
func (s *Store) RemoveAccount(ctx context.Context, address string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mailboxes SET is_account = 0, password = NULL WHERE address = ? AND is_account = 1`,
		NormalizeAddress(address))
	if err != nil {
		return fmt.Errorf("remove account: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.publish(events.MailboxUpdated, MailboxEvent{Address: NormalizeAddress(address)})
	return nil
}

// CheckPassword verifies login credentials for strict auth. Accounts with
// no password accept any password; unknown addresses are rejected.
func (s *Store) CheckPassword(ctx context.Context, address, password string) (bool, error) {
	var stored sql.NullString
	var isAccount bool
	err := s.db.QueryRowContext(ctx, `SELECT is_account, password FROM mailboxes WHERE address = ?`,
		NormalizeAddress(address)).Scan(&isAccount, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check password: %w", err)
	}
	if !isAccount {
		return false, nil
	}
	if !stored.Valid {
		return true, nil
	}
	return subtle.ConstantTimeCompare([]byte(stored.String), []byte(password)) == 1, nil
}
