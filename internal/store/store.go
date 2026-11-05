// Package store persists messages, mailboxes and deliveries in SQLite and
// publishes every change on the events bus.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite")

	"github.com/arifurrahmanns/envry/internal/events"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a message, mailbox or delivery does not exist.
var ErrNotFound = errors.New("not found")

// Options configures Open.
type Options struct {
	// Path is a database file path or ":memory:".
	Path string
	// MaxMessages keeps only the newest N messages; 0 disables pruning.
	MaxMessages int
	Bus         *events.Bus
	Logger      *slog.Logger
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Store is the SQLite-backed message store. It is safe for concurrent use.
type Store struct {
	db          *sql.DB
	bus         *events.Bus
	log         *slog.Logger
	maxMessages int
	now         func() time.Time

	idMu            sync.Mutex
	entropy         io.Reader
	lastUIDValidity uint32
}

// Open opens (creating if needed) the database and applies migrations.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.Bus == nil {
		opts.Bus = events.New()
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	path := opts.Path
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	// One connection: SQLite serialises writers anyway, and an in-memory
	// database exists only within a single connection.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{
		db:          db,
		bus:         opts.Bus,
		log:         opts.Logger,
		maxMessages: opts.MaxMessages,
		now:         opts.Now,
		entropy:     ulid.Monotonic(rand.Reader, 0),
	}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, mustSub(migrations, "migrations"))
	if err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Bus returns the bus the store publishes on.
func (s *Store) Bus() *events.Bus { return s.bus }

func (s *Store) newID(t time.Time) string {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), s.entropy).String()
}

func (s *Store) publish(typ string, data any) {
	s.bus.Publish(events.Event{Type: typ, Data: data})
}

// inTx runs fn in a transaction, committing on success.
func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func millis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullMillis(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := fromMillis(ms.Int64)
	return &t
}

func nullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func toNull(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// likeContains builds a LIKE pattern matching s anywhere, escaped with '\'.
func likeContains(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// ftsQuery turns free text into an FTS5 query in which every word must
// match as a prefix. Words are quoted so FTS5 syntax characters are literal.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(words, " ")
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // dir is a constant embedded path
	}
	return sub
}

// NormalizeAddress lower-cases and trims an email address.
func NormalizeAddress(a string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(strings.TrimSpace(a), "<>")))
}
