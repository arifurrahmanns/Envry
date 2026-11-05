package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arifurrahmanns/envry/internal/events"
)

// MaildropMessage is one message in a POP3 maildrop.
type MaildropMessage struct {
	DeliveryID int64
	MessageID  string
	// WireSize is the size in octets with CRLF line endings.
	WireSize int64
}

// UniqueID is the POP3 UIDL value: stable across sessions and restarts, and
// unique even if the same message is filed into a folder twice.
func (m MaildropMessage) UniqueID() string {
	return fmt.Sprintf("%s-%d", m.MessageID, m.DeliveryID)
}

// Maildrop lists a mailbox's INBOX, oldest first, as POP3 numbers it. An
// address that has never received mail has an empty maildrop.
func (s *Store) Maildrop(ctx context.Context, address string) ([]MaildropMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, m.id, m.wire_size
		FROM deliveries d
		JOIN folders f ON f.id = d.folder_id
		JOIN mailboxes mb ON mb.id = f.mailbox_id
		JOIN messages m ON m.id = d.message_id
		WHERE mb.address = ? AND f.name = ? AND d.deleted = 0
		ORDER BY d.uid`, NormalizeAddress(address), FolderInbox)
	if err != nil {
		return nil, fmt.Errorf("load maildrop: %w", err)
	}
	defer rows.Close()
	var out []MaildropMessage
	for rows.Next() {
		var m MaildropMessage
		if err := rows.Scan(&m.DeliveryID, &m.MessageID, &m.WireSize); err != nil {
			return nil, fmt.Errorf("scan maildrop: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteDeliveries removes several mailbox copies in one transaction and
// reports how many existed.
func (s *Store) DeleteDeliveries(ctx context.Context, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted []DeliveryEvent
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rows, err := tx.Query(`
			SELECT d.id, d.message_id, mb.address, f.name, d.seen, d.flagged
			FROM deliveries d
			JOIN folders f ON f.id = d.folder_id
			JOIN mailboxes mb ON mb.id = f.mailbox_id
			WHERE d.id IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var e DeliveryEvent
			if err := rows.Scan(&e.DeliveryID, &e.MessageID, &e.Mailbox, &e.Folder, &e.Seen, &e.Flagged); err != nil {
				rows.Close()
				return err
			}
			deleted = append(deleted, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM deliveries WHERE id IN (`+placeholders+`)`, args...)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete deliveries: %w", err)
	}
	for _, e := range deleted {
		s.publish(events.DeliveryDeleted, e)
	}
	return len(deleted), nil
}
