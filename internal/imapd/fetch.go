package imapd

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"slices"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-message/textproto"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
	"github.com/arifurrahmanns/envry/internal/store"
)

// content loads a message's raw bytes on first use, in the CRLF form whose
// size RFC822.SIZE reports.
type content struct {
	st     *store.Store
	id     string
	raw    []byte
	err    error
	loaded bool
}

func (c *content) bytes() ([]byte, error) {
	if !c.loaded {
		c.loaded = true
		raw, err := c.st.Raw(ctx(), c.id)
		c.raw, c.err = mimeparse.CRLF(raw), err
	}
	return c.raw, c.err
}

func (ss *session) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	sel := ss.sel
	f := sel.folder
	// BODY[...] without .PEEK marks the message \Seen (not in EXAMINE).
	markSeen := !sel.readOnly && slices.ContainsFunc(options.BodySection, func(bs *imap.FetchItemBodySection) bool {
		return !bs.Peek
	})

	f.mu.Lock()
	defer f.mu.Unlock()
	return sel.forEach(numSet, func(seqNum uint32, i int) error {
		m := &f.msgs[i]
		flagsChanged := false
		if markSeen && !hasFlag(m.Flags, imap.FlagSeen) {
			next := append(slices.Clone(m.Flags), string(imap.FlagSeen))
			if err := ss.srv.store.SetDeliveryFlags(ctx(), m.DeliveryID, next); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			m.Flags = next
			flagsChanged = true
			f.tracker.QueueMessageFlags(seqNum, imap.UID(m.UID), imapFlags(next), sel.tracker)
		}
		rw := w.CreateMessage(sel.tracker.EncodeSeqNum(seqNum))
		return writeMessage(rw, *m, options, flagsChanged, &content{st: ss.srv.store, id: m.MessageID})
	})
}

func writeMessage(w *imapserver.FetchResponseWriter, m store.FolderMessage, options *imap.FetchOptions, flagsChanged bool, c *content) error {
	w.WriteUID(imap.UID(m.UID))
	// When FETCH sets \Seen, the new flags are sent even if not requested.
	if options.Flags || flagsChanged {
		w.WriteFlags(imapFlags(m.Flags))
	}
	if options.InternalDate {
		w.WriteInternalDate(m.Date)
	}
	if options.RFC822Size {
		w.WriteRFC822Size(m.Size)
	}

	needsContent := options.Envelope || options.BodyStructure != nil || len(options.BodySection) > 0 ||
		len(options.BinarySection) > 0 || len(options.BinarySectionSize) > 0
	if needsContent {
		raw, err := c.bytes()
		if errors.Is(err, store.ErrNotFound) {
			return w.Close() // expunged meanwhile: send what we have
		}
		if err != nil {
			return err
		}
		if options.Envelope {
			if h, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(raw))); err == nil {
				w.WriteEnvelope(imapserver.ExtractEnvelope(h))
			}
		}
		if options.BodyStructure != nil {
			w.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(raw)))
		}
		for _, bs := range options.BodySection {
			data := imapserver.ExtractBodySection(bytes.NewReader(raw), bs)
			if err := writeLiteral(w.WriteBodySection(bs, int64(len(data))), data); err != nil {
				return err
			}
		}
		for _, bs := range options.BinarySection {
			data := imapserver.ExtractBinarySection(bytes.NewReader(raw), bs)
			if err := writeLiteral(w.WriteBinarySection(bs, int64(len(data))), data); err != nil {
				return err
			}
		}
		for _, bss := range options.BinarySectionSize {
			w.WriteBinarySectionSize(bss, imapserver.ExtractBinarySectionSize(bytes.NewReader(raw), bss))
		}
	}
	return w.Close()
}

func writeLiteral(wc io.WriteCloser, data []byte) error {
	_, err := wc.Write(data)
	if cerr := wc.Close(); err == nil {
		err = cerr
	}
	return err
}
