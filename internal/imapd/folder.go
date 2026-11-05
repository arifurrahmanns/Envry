package imapd

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"

	"github.com/arifurrahmanns/envry/internal/store"
)

// folder is the shared in-memory view of one selected folder: its messages
// in UID order (position = sequence number) and the tracker that queues
// unilateral updates for each session.
type folder struct {
	id      int64
	tracker *imapserver.MailboxTracker
	refs    int // guarded by Server.mu

	mu   sync.Mutex // guards msgs; held for every read and write
	msgs []store.FolderMessage
}

// sync reconciles the view with the store and queues EXPUNGE, FETCH FLAGS
// and EXISTS updates for every session. Callers hold f.mu.
func (f *folder) sync(ctx context.Context, st *store.Store) error {
	cur, err := st.FolderMessages(ctx, f.id)
	if err != nil {
		return err
	}
	byUID := make(map[uint32]store.FolderMessage, len(cur))
	for _, m := range cur {
		byUID[m.UID] = m
	}

	// Expunges, highest sequence number first so earlier ones stay valid.
	for i := len(f.msgs) - 1; i >= 0; i-- {
		if _, ok := byUID[f.msgs[i].UID]; !ok {
			f.tracker.QueueExpunge(uint32(i + 1))
			f.msgs = slices.Delete(f.msgs, i, i+1)
		}
	}

	for i := range f.msgs {
		now := byUID[f.msgs[i].UID]
		if !sameFlags(now.Flags, f.msgs[i].Flags) {
			f.msgs[i].Flags = now.Flags
			f.tracker.QueueMessageFlags(uint32(i+1), imap.UID(now.UID), imapFlags(now.Flags), nil)
		}
	}

	var last uint32
	if n := len(f.msgs); n > 0 {
		last = f.msgs[n-1].UID
	}
	added := false
	for _, m := range cur { // UIDs only grow, so new messages sort last
		if m.UID > last {
			f.msgs = append(f.msgs, m)
			added = true
		}
	}
	if added {
		f.tracker.QueueNumMessages(uint32(len(f.msgs)))
	}
	return nil
}

func (f *folder) lastUID() uint32 {
	if len(f.msgs) == 0 {
		return 0
	}
	return f.msgs[len(f.msgs)-1].UID
}

func imapFlags(flags []string) []imap.Flag {
	out := make([]imap.Flag, len(flags))
	for i, f := range flags {
		out[i] = imap.Flag(f)
	}
	return out
}

func storeFlags(flags []imap.Flag) []string {
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = string(f)
	}
	return out
}

func hasFlag(flags []string, flag imap.Flag) bool {
	for _, f := range flags {
		if strings.EqualFold(f, string(flag)) {
			return true
		}
	}
	return false
}

func sameFlags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, f := range a {
		if !hasFlag(b, imap.Flag(f)) {
			return false
		}
	}
	return true
}

// applyStore computes a message's flags after a STORE operation.
func applyStore(cur []string, op *imap.StoreFlags) []string {
	switch op.Op {
	case imap.StoreFlagsSet:
		return storeFlags(op.Flags)
	case imap.StoreFlagsAdd:
		out := slices.Clone(cur)
		for _, f := range op.Flags {
			if !hasFlag(out, f) {
				out = append(out, string(f))
			}
		}
		return out
	case imap.StoreFlagsDel:
		out := make([]string, 0, len(cur))
		for _, f := range cur {
			if !slices.ContainsFunc(op.Flags, func(d imap.Flag) bool { return strings.EqualFold(f, string(d)) }) {
				out = append(out, f)
			}
		}
		return out
	}
	return cur
}

// systemFlags are always offered in FLAGS and PERMANENTFLAGS.
var systemFlags = []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft}

// definedFlags lists the system flags plus every keyword in use.
func (f *folder) definedFlags() []imap.Flag {
	out := slices.Clone(systemFlags)
	for _, m := range f.msgs {
		for _, fl := range m.Flags {
			if !strings.HasPrefix(fl, `\`) && !hasFlag(storeFlags(out), imap.Flag(fl)) {
				out = append(out, imap.Flag(fl))
			}
		}
	}
	return out
}
