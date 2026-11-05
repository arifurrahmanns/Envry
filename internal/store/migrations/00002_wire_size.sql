-- +goose Up

-- Size in octets with CRLF line endings: what POP3 LIST and IMAP
-- RFC822.SIZE report. Mail captured so far arrived over SMTP or the API,
-- both of which already use CRLF.
ALTER TABLE messages ADD COLUMN wire_size INTEGER NOT NULL DEFAULT 0;
UPDATE messages SET wire_size = size;

-- +goose Down
ALTER TABLE messages DROP COLUMN wire_size;
