package imapd

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"

	"github.com/arifurrahmanns/envry/internal/store"
)

const delim = '/'

type session struct {
	srv     *Server
	conn    *imapserver.Conn
	address string     // mailbox, set by Login
	sel     *selection // selected folder, if any
}

// selection is one session's view of its selected folder.
type selection struct {
	folder    *folder
	info      store.MailFolder
	tracker   *imapserver.SessionTracker
	readOnly  bool
	searchRes imap.UIDSet
}

var (
	_ imapserver.SessionIMAP4rev2   = (*session)(nil)
	_ imapserver.SessionAppendLimit = (*session)(nil)
)

func ctx() context.Context { return context.Background() }

func no(code imap.ResponseCode, text string) error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Code: code, Text: text}
}

var (
	errNoSuchMailbox = no(imap.ResponseCodeNonExistent, "No such mailbox")
	errTryCreate     = no(imap.ResponseCodeTryCreate, "No such mailbox")
	errReadOnly      = no("READ-ONLY", "Mailbox is read-only")
)

// folderError maps store folder errors to IMAP responses.
func folderError(err error) error {
	switch {
	case errors.Is(err, store.ErrFolderNotFound):
		return errNoSuchMailbox
	case errors.Is(err, store.ErrFolderExists):
		return no(imap.ResponseCodeAlreadyExists, "Mailbox already exists")
	case errors.Is(err, store.ErrFolderProtected):
		return no(imap.ResponseCodeCannot, "INBOX cannot be deleted or renamed")
	}
	return err
}

func (ss *session) Close() error {
	ss.unselect()
	return nil
}

func (ss *session) Login(username, password string) error {
	addr := store.NormalizeAddress(username)
	if addr == "" {
		return imapserver.ErrAuthFailed
	}
	if ss.srv.opts.StrictAuth {
		ok, err := ss.srv.store.CheckPassword(ctx(), addr, password)
		if err != nil {
			return err
		}
		if !ok {
			return imapserver.ErrAuthFailed
		}
	}
	if err := ss.srv.store.EnsureMailbox(ctx(), addr); err != nil {
		return err
	}
	ss.address = addr
	return nil
}

func (ss *session) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	ss.unselect()
	info, err := ss.srv.store.MailFolder(ctx(), ss.address, name)
	if err != nil {
		return nil, folderError(err)
	}
	f, err := ss.srv.acquire(ctx(), info.ID)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.sync(ctx(), ss.srv.store); err != nil {
		ss.srv.release(f)
		return nil, err
	}
	ss.sel = &selection{folder: f, info: *info, tracker: f.tracker.NewSession(), readOnly: options.ReadOnly}

	var firstUnseen uint32
	for i, m := range f.msgs {
		if !hasFlag(m.Flags, imap.FlagSeen) {
			firstUnseen = uint32(i + 1)
			break
		}
	}
	permanent := []imap.Flag{}
	if !options.ReadOnly {
		permanent = append(slices.Clone(systemFlags), imap.FlagWildcard)
	}
	return &imap.SelectData{
		Flags:             f.definedFlags(),
		PermanentFlags:    permanent,
		NumMessages:       uint32(len(f.msgs)),
		FirstUnseenSeqNum: firstUnseen,
		UIDNext:           imap.UID(max(info.UIDNext, f.lastUID()+1)),
		UIDValidity:       info.UIDValidity,
	}, nil
}

func (ss *session) Unselect() error {
	ss.unselect()
	return nil
}

func (ss *session) unselect() {
	if ss.sel == nil {
		return
	}
	ss.sel.tracker.Close()
	ss.srv.release(ss.sel.folder)
	ss.sel = nil
}

func (ss *session) Create(name string, _ *imap.CreateOptions) error {
	return folderError(ss.srv.store.CreateFolder(ctx(), ss.address, name))
}

func (ss *session) Delete(name string) error {
	return folderError(ss.srv.store.DeleteFolder(ctx(), ss.address, name))
}

func (ss *session) Rename(oldName, newName string, _ *imap.RenameOptions) error {
	return folderError(ss.srv.store.RenameFolder(ctx(), ss.address, oldName, newName))
}

func (ss *session) Subscribe(name string) error {
	return folderError(ss.srv.store.SetFolderSubscribed(ctx(), ss.address, name, true))
}

func (ss *session) Unsubscribe(name string) error {
	return folderError(ss.srv.store.SetFolderSubscribed(ctx(), ss.address, name, false))
}

func (ss *session) Namespace() (*imap.NamespaceData, error) {
	return &imap.NamespaceData{Personal: []imap.NamespaceDescriptor{{Prefix: "", Delim: delim}}}, nil
}

func (ss *session) AppendLimit() uint32 { return ss.srv.appendLimit() }

var specialUseAttrs = map[string]imap.MailboxAttr{
	`\Sent`:    imap.MailboxAttrSent,
	`\Drafts`:  imap.MailboxAttrDrafts,
	`\Trash`:   imap.MailboxAttrTrash,
	`\Junk`:    imap.MailboxAttrJunk,
	`\Archive`: imap.MailboxAttrArchive,
}

func (ss *session) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	if len(patterns) == 0 {
		// LIST "" "" asks for the hierarchy delimiter.
		return w.WriteList(&imap.ListData{Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}, Delim: delim})
	}
	folders, err := ss.srv.store.MailFolders(ctx(), ss.address)
	if err != nil {
		return err
	}
	for _, f := range folders {
		if !slices.ContainsFunc(patterns, func(p string) bool { return imapserver.MatchList(f.Name, delim, ref, p) }) {
			continue
		}
		if options.SelectSubscribed && !f.Subscribed {
			continue
		}
		if options.SelectSpecialUse && f.SpecialUse == "" {
			continue
		}

		data := &imap.ListData{Mailbox: f.Name, Delim: delim}
		hasChildren := slices.ContainsFunc(folders, func(o store.MailFolder) bool {
			return strings.HasPrefix(o.Name, f.Name+string(delim))
		})
		if hasChildren {
			data.Attrs = append(data.Attrs, imap.MailboxAttrHasChildren)
		} else {
			data.Attrs = append(data.Attrs, imap.MailboxAttrHasNoChildren)
		}
		if f.Subscribed && (options.SelectSubscribed || options.ReturnSubscribed) {
			data.Attrs = append(data.Attrs, imap.MailboxAttrSubscribed)
		}
		// Special-use attributes are always returned (RFC 6154 allows it),
		// so clients that do not ask still find Sent, Drafts and Trash.
		if attr, ok := specialUseAttrs[f.SpecialUse]; ok {
			data.Attrs = append(data.Attrs, attr)
		}
		if options.ReturnStatus != nil {
			if data.Status, err = ss.status(f, options.ReturnStatus); err != nil {
				return err
			}
		}
		if err := w.WriteList(data); err != nil {
			return err
		}
	}
	return nil
}

func (ss *session) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	f, err := ss.srv.store.MailFolder(ctx(), ss.address, name)
	if err != nil {
		return nil, folderError(err)
	}
	return ss.status(*f, options)
}

func (ss *session) status(f store.MailFolder, options *imap.StatusOptions) (*imap.StatusData, error) {
	msgs, err := ss.srv.store.FolderMessages(ctx(), f.ID)
	if err != nil {
		return nil, err
	}
	data := &imap.StatusData{Mailbox: f.Name}
	var unseen, deleted uint32
	var size int64
	var last uint32
	for _, m := range msgs {
		if !hasFlag(m.Flags, imap.FlagSeen) {
			unseen++
		}
		if hasFlag(m.Flags, imap.FlagDeleted) {
			deleted++
		}
		size += m.Size
		last = max(last, m.UID)
	}
	n := uint32(len(msgs))
	if options.NumMessages {
		data.NumMessages = &n
	}
	if options.UIDNext {
		data.UIDNext = imap.UID(max(f.UIDNext, last+1))
	}
	if options.UIDValidity {
		data.UIDValidity = f.UIDValidity
	}
	if options.NumUnseen {
		data.NumUnseen = &unseen
	}
	if options.NumDeleted {
		data.NumDeleted = &deleted
	}
	if options.Size {
		data.Size = &size
	}
	if options.NumRecent {
		zero := uint32(0)
		data.NumRecent = &zero
	}
	if options.AppendLimit {
		limit := ss.srv.appendLimit()
		data.AppendLimit = &limit
	}
	return data, nil
}

func (ss *session) Append(name string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	limit := int64(ss.srv.appendLimit())
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, no(imap.ResponseCodeTooBig, "Message exceeds the size limit")
	}
	uid, validity, err := ss.srv.store.AppendMessage(ctx(), store.AppendInput{
		Address: ss.address,
		Folder:  name,
		Raw:     raw,
		Flags:   storeFlags(options.Flags),
		Date:    options.Time,
	})
	if errors.Is(err, store.ErrFolderNotFound) {
		return nil, errTryCreate
	}
	if err != nil {
		return nil, err
	}
	return &imap.AppendData{UID: imap.UID(uid), UIDValidity: validity}, nil
}

func (ss *session) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	if ss.sel == nil {
		return nil
	}
	f := ss.sel.folder
	f.mu.Lock()
	err := f.sync(ctx(), ss.srv.store)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return ss.sel.tracker.Poll(w, allowExpunge)
}

func (ss *session) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if ss.sel == nil {
		<-stop
		return nil
	}
	if err := ss.Poll(w, true); err != nil {
		return err
	}
	return ss.sel.tracker.Idle(w, stop)
}

// --- Selected state ---

// forEach calls fn for each message in numSet, in UID order, with its
// server-side sequence number and index. Callers hold the folder lock.
func (sel *selection) forEach(numSet imap.NumSet, fn func(seqNum uint32, i int) error) error {
	numSet = sel.static(numSet)
	for i, m := range sel.folder.msgs {
		seqNum := uint32(i + 1)
		switch set := numSet.(type) {
		case imap.SeqSet:
			client := sel.tracker.EncodeSeqNum(seqNum)
			if client == 0 || !set.Contains(client) {
				continue
			}
		case imap.UIDSet:
			if !set.Contains(imap.UID(m.UID)) {
				continue
			}
		}
		if err := fn(seqNum, i); err != nil {
			return err
		}
	}
	return nil
}

// static resolves "*" (the last message) and "$" (the saved search result).
func (sel *selection) static(numSet imap.NumSet) imap.NumSet {
	if imap.IsSearchRes(numSet) {
		return sel.searchRes
	}
	switch set := numSet.(type) {
	case imap.SeqSet:
		last := uint32(len(sel.folder.msgs))
		for i := range set {
			resolveStar(&set[i].Start, &set[i].Stop, last)
		}
	case imap.UIDSet:
		last := sel.folder.lastUID()
		for i := range set {
			resolveStar((*uint32)(&set[i].Start), (*uint32)(&set[i].Stop), last)
		}
	}
	return numSet
}

func resolveStar(start, stop *uint32, last uint32) {
	dynamic := *start == 0 || *stop == 0
	if *start == 0 {
		*start = last
	}
	if *stop == 0 {
		*stop = last
	}
	if dynamic && *start > *stop {
		*start, *stop = *stop, *start
	}
}

func (ss *session) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	sel := ss.sel
	if sel.readOnly {
		return errReadOnly
	}
	f := sel.folder
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int64
	for _, m := range f.msgs {
		if hasFlag(m.Flags, imap.FlagDeleted) && (uids == nil || uids.Contains(imap.UID(m.UID))) {
			ids = append(ids, m.DeliveryID)
		}
	}
	if _, err := ss.srv.store.DeleteDeliveries(ctx(), ids); err != nil {
		return err
	}
	// The sync queues the EXPUNGE responses for every session, this one
	// included; they are written right after this command.
	return f.sync(ctx(), ss.srv.store)
}

func (ss *session) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, _ *imap.StoreOptions) error {
	sel := ss.sel
	if sel.readOnly {
		return errReadOnly
	}
	f := sel.folder
	f.mu.Lock()
	defer f.mu.Unlock()
	return sel.forEach(numSet, func(seqNum uint32, i int) error {
		m := &f.msgs[i]
		next := applyStore(m.Flags, flags)
		if !sameFlags(next, m.Flags) {
			if err := ss.srv.store.SetDeliveryFlags(ctx(), m.DeliveryID, next); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			m.Flags = next
			f.tracker.QueueMessageFlags(seqNum, imap.UID(m.UID), imapFlags(next), sel.tracker)
		}
		if flags.Silent {
			return nil
		}
		rw := w.CreateMessage(sel.tracker.EncodeSeqNum(seqNum))
		rw.WriteUID(imap.UID(m.UID))
		rw.WriteFlags(imapFlags(m.Flags))
		return rw.Close()
	})
}

func (ss *session) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	data, _, err := ss.copy(numSet, dest, false)
	return data, err
}

func (ss *session) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
	data, f, err := ss.copy(numSet, dest, true)
	if err != nil {
		return err
	}
	f.mu.Lock()
	err = f.sync(ctx(), ss.srv.store) // queues the EXPUNGEs, written after COPYUID
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return w.WriteCopyData(data)
}

func (ss *session) copy(numSet imap.NumSet, destName string, move bool) (*imap.CopyData, *folder, error) {
	sel := ss.sel
	if move && sel.readOnly {
		return nil, nil, errReadOnly
	}
	dest, err := ss.srv.store.MailFolder(ctx(), ss.address, destName)
	if err != nil {
		if errors.Is(err, store.ErrFolderNotFound) {
			return nil, nil, errTryCreate
		}
		return nil, nil, err
	}
	if move && dest.ID == sel.info.ID {
		return nil, nil, no(imap.ResponseCodeCannot, "Source and destination are the same mailbox")
	}

	f := sel.folder
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int64
	var src imap.UIDSet
	_ = sel.forEach(numSet, func(_ uint32, i int) error {
		ids = append(ids, f.msgs[i].DeliveryID)
		src.AddNum(imap.UID(f.msgs[i].UID))
		return nil
	})
	if len(ids) == 0 {
		return &imap.CopyData{UIDValidity: dest.UIDValidity}, f, nil
	}
	uids, err := ss.srv.store.CopyDeliveries(ctx(), ids, dest.ID, move)
	if err != nil {
		return nil, nil, err
	}
	var dst imap.UIDSet
	for _, u := range uids {
		dst.AddNum(imap.UID(u))
	}
	return &imap.CopyData{UIDValidity: dest.UIDValidity, SourceUIDs: src, DestUIDs: dst}, f, nil
}
