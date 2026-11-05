-- +goose Up

-- Every message captured by SMTP, appended over IMAP, or sent through the API.
-- Timestamps are Unix milliseconds. Address columns hold JSON.
CREATE TABLE messages (
  id               TEXT PRIMARY KEY,              -- ULID, sortable by time
  message_id_hdr   TEXT,                          -- Message-ID header
  envelope_from    TEXT NOT NULL,                 -- MAIL FROM
  recipients       TEXT NOT NULL DEFAULT '[]',    -- RCPT TO list: [{address, type}]
  from_hdr         TEXT NOT NULL DEFAULT '{}',    -- {name, address}
  to_hdr           TEXT NOT NULL DEFAULT '[]',    -- [{name, address}]
  cc_hdr           TEXT NOT NULL DEFAULT '[]',
  reply_to_hdr     TEXT NOT NULL DEFAULT '[]',
  subject          TEXT NOT NULL DEFAULT '',
  date_hdr         INTEGER,                       -- Date header, NULL if missing
  preview          TEXT NOT NULL DEFAULT '',
  size             INTEGER NOT NULL,
  has_html         INTEGER NOT NULL DEFAULT 0,
  has_text         INTEGER NOT NULL DEFAULT 0,
  attachment_count INTEGER NOT NULL DEFAULT 0,
  source           TEXT NOT NULL,                 -- smtp | imap_append | api
  client_ip        TEXT,
  auth_user        TEXT,                          -- SMTP AUTH username, if any
  received_at      INTEGER NOT NULL
);

CREATE TABLE message_raw (
  message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  raw        BLOB NOT NULL                        -- exact bytes received (.eml)
);

CREATE TABLE attachments (
  message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  id           TEXT NOT NULL,                     -- "1", "2", … within the message
  filename     TEXT NOT NULL DEFAULT '',
  content_type TEXT NOT NULL,
  content_id   TEXT,                              -- for cid: inline images
  inline       INTEGER NOT NULL DEFAULT 0,
  size         INTEGER NOT NULL,
  part_path    TEXT NOT NULL,                     -- MIME path, e.g. "2.1"
  PRIMARY KEY (message_id, id)
);

-- One per address ever seen; is_account = 1 shows it under Inbox.
CREATE TABLE mailboxes (
  id           INTEGER PRIMARY KEY,
  address      TEXT NOT NULL UNIQUE,              -- lower-cased
  display_name TEXT,
  is_account   INTEGER NOT NULL DEFAULT 0,
  password     TEXT,                              -- NULL = accept any password
  created_at   INTEGER NOT NULL
);

CREATE TABLE folders (
  id           INTEGER PRIMARY KEY,
  mailbox_id   INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,                     -- INBOX, Sent, Drafts, Trash, custom
  special_use  TEXT,                              -- \Sent, \Drafts, \Trash
  uid_validity INTEGER NOT NULL,
  uid_next     INTEGER NOT NULL DEFAULT 1,
  subscribed   INTEGER NOT NULL DEFAULT 1,
  UNIQUE (mailbox_id, name)
);

CREATE TABLE deliveries (
  id           INTEGER PRIMARY KEY,
  message_id   TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  folder_id    INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
  uid          INTEGER NOT NULL,
  rcpt_type    TEXT NOT NULL,                     -- to | cc | bcc | self (Sent copy)
  seen         INTEGER NOT NULL DEFAULT 0,
  flagged      INTEGER NOT NULL DEFAULT 0,
  answered     INTEGER NOT NULL DEFAULT 0,
  draft        INTEGER NOT NULL DEFAULT 0,
  deleted      INTEGER NOT NULL DEFAULT 0,        -- \Deleted until EXPUNGE
  keywords     TEXT NOT NULL DEFAULT '[]',        -- JSON custom flags
  delivered_at INTEGER NOT NULL,
  UNIQUE (folder_id, uid)
);

CREATE INDEX idx_messages_received ON messages(received_at DESC);
CREATE INDEX idx_deliveries_folder ON deliveries(folder_id, uid);
CREATE INDEX idx_deliveries_message ON deliveries(message_id);

-- Full-text search for the UI and IMAP SEARCH.
CREATE VIRTUAL TABLE messages_fts USING fts5(
  message_id UNINDEXED, subject, from_hdr, to_hdr, body, tokenize='unicode61'
);

-- +goose StatementBegin
CREATE TRIGGER messages_fts_delete AFTER DELETE ON messages BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER messages_fts_delete;
DROP TABLE messages_fts;
DROP TABLE deliveries;
DROP TABLE folders;
DROP TABLE mailboxes;
DROP TABLE attachments;
DROP TABLE message_raw;
DROP TABLE messages;
