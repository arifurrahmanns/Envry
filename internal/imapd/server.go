// Package imapd is the IMAP server. It serves every mailbox's folders from
// the store with go-imap's imapserver, and keeps connected clients in sync
// with changes made over SMTP, POP3, the web UI or other IMAP sessions.
//
// The username is the mailbox address. Any password is accepted unless
// strict auth is on.
package imapd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
	"sync/atomic"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"

	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/store"
)

// Options configures the server.
type Options struct {
	// StrictAuth requires an account's password at login.
	StrictAuth bool
	// MaxMessageBytes limits APPEND uploads (APPENDLIMIT).
	MaxMessageBytes int64
	Logger          *slog.Logger
}

// Server is an IMAP server backed by the store.
type Server struct {
	store *store.Store
	opts  Options
	log   *slog.Logger
	imap  *imapserver.Server

	mu      sync.Mutex
	folders map[int64]*folder // folders selected by at least one session

	closed  atomic.Bool
	hubOnce sync.Once
	stopHub func()
	hubDone chan struct{}
}

// New builds a server; call Serve to accept connections.
func New(st *store.Store, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{
		store:   st,
		opts:    opts,
		log:     opts.Logger,
		folders: map[int64]*folder{},
		stopHub: func() {},
		hubDone: make(chan struct{}),
	}
	caps := imap.CapSet{}
	for _, c := range []imap.Cap{
		imap.CapIMAP4rev1, imap.CapNamespace, imap.CapUIDPlus, imap.CapMove, imap.CapSpecialUse,
		imap.CapChildren, imap.CapListExtended, imap.CapListStatus, imap.CapStatusSize,
		imap.CapESearch, imap.CapSearchRes, imap.CapAppendLimit,
	} {
		caps[c] = struct{}{}
	}
	s.imap = imapserver.New(&imapserver.Options{
		NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{srv: s, conn: c}, nil, nil
		},
		Caps:   caps,
		Logger: logAdapter{opts.Logger},
		// Local test server without TLS: LOGIN over plain text is expected.
		InsecureAuth: true,
	})
	return s
}

// Serve accepts connections on ln until Shutdown.
func (s *Server) Serve(ln net.Listener) error {
	s.hubOnce.Do(s.startHub)
	err := s.imap.Serve(ln)
	if s.closed.Load() {
		return nil
	}
	return err
}

// Shutdown closes the listener and every connection.
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := s.imap.Close()
	s.hubOnce.Do(func() { close(s.hubDone) }) // hub never started
	s.stopHub()
	select {
	case <-s.hubDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("imap shutdown: %w", err)
	}
	return nil
}

func (s *Server) appendLimit() uint32 {
	if s.opts.MaxMessageBytes <= 0 || s.opts.MaxMessageBytes > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(s.opts.MaxMessageBytes)
}

// startHub re-syncs selected folders whenever mail changes elsewhere, which
// is what pushes EXISTS / EXPUNGE / FETCH updates to idling clients.
func (s *Server) startHub() {
	ch, cancel := s.store.Bus().Subscribe(256)
	s.stopHub = cancel
	go func() {
		defer close(s.hubDone)
		for ev := range ch {
			if !affectsFolders(ev.Type) {
				continue
			}
			// Coalesce a burst of events into one sync.
		drain:
			for {
				select {
				case next, ok := <-ch:
					if !ok {
						break drain
					}
					_ = next
				default:
					break drain
				}
			}
			s.syncAll()
		}
	}()
}

func affectsFolders(typ string) bool {
	switch typ {
	case events.MessageCreated, events.MessageDeleted, events.MessagesCleared,
		events.DeliveryCreated, events.DeliveryUpdated, events.DeliveryDeleted, events.MailboxUpdated:
		return true
	}
	return false
}

func (s *Server) syncAll() {
	s.mu.Lock()
	list := make([]*folder, 0, len(s.folders))
	for _, f := range s.folders {
		list = append(list, f)
	}
	s.mu.Unlock()
	for _, f := range list {
		f.mu.Lock()
		if err := f.sync(context.Background(), s.store); err != nil {
			s.log.Warn("imap folder sync failed", "folder", f.id, "err", err)
		}
		f.mu.Unlock()
	}
}

// acquire returns the shared state of a folder, loading it on first use.
func (s *Server) acquire(ctx context.Context, id int64) (*folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.folders[id]; f != nil {
		f.refs++
		return f, nil
	}
	msgs, err := s.store.FolderMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	f := &folder{
		id:      id,
		msgs:    msgs,
		tracker: imapserver.NewMailboxTracker(uint32(len(msgs))),
		refs:    1,
	}
	s.folders[id] = f
	return f, nil
}

func (s *Server) release(f *folder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.refs--
	if f.refs <= 0 {
		delete(s.folders, f.id)
	}
}

type logAdapter struct{ log *slog.Logger }

func (l logAdapter) Printf(format string, args ...any) {
	l.log.Warn("imap: " + fmt.Sprintf(format, args...))
}
