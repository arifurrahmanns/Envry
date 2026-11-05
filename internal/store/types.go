package store

import (
	"time"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// The JSON shapes below are the /api/v1 contract mirrored by
// web/src/lib/api.ts; keep the two in sync.

// Address is a display name plus email address.
type Address = mimeparse.Address

// Header is one decoded header field.
type Header = mimeparse.Header

// Message sources.
const (
	SourceSMTP       = "smtp"
	SourceIMAPAppend = "imap_append"
	SourceAPI        = "api"
)

// Recipient types. RcptSelf marks the copy in the sender's own Sent folder.
const (
	RcptTo   = "to"
	RcptCc   = "cc"
	RcptBcc  = "bcc"
	RcptSelf = "self"
)

// Folder names created for every mailbox.
const (
	FolderInbox  = "INBOX"
	FolderSent   = "Sent"
	FolderDrafts = "Drafts"
	FolderTrash  = "Trash"
)

// MessageSummary is one row in a message list.
type MessageSummary struct {
	ID              string     `json:"id"`
	From            Address    `json:"from"`
	To              []Address  `json:"to"`
	Cc              []Address  `json:"cc"`
	Subject         string     `json:"subject"`
	Preview         string     `json:"preview"`
	Date            *time.Time `json:"date"`
	ReceivedAt      time.Time  `json:"receivedAt"`
	Size            int64      `json:"size"`
	HasHTML         bool       `json:"hasHtml"`
	HasText         bool       `json:"hasText"`
	AttachmentCount int        `json:"attachmentCount"`
	Source          string     `json:"source"`
}

// Recipient is one envelope recipient (RCPT TO) and how it was addressed.
type Recipient struct {
	Address string `json:"address"`
	Type    string `json:"type"`
}

// Attachment describes a stored attachment.
type Attachment struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"contentType"`
	ContentID   *string `json:"contentId"`
	Inline      bool    `json:"inline"`
	Size        int64   `json:"size"`
	PartPath    string  `json:"-"`
}

// MessageDetail is a full message as shown in the reader.
type MessageDetail struct {
	MessageSummary
	MessageID    *string      `json:"messageId"`
	EnvelopeFrom string       `json:"envelopeFrom"`
	ReplyTo      []Address    `json:"replyTo"`
	Recipients   []Recipient  `json:"recipients"`
	Headers      []Header     `json:"headers"`
	Text         *string      `json:"text"`
	ClientIP     *string      `json:"clientIp"`
	AuthUser     *string      `json:"authUser"`
	Attachments  []Attachment `json:"attachments"`
}

// Delivery is a message as delivered into one mailbox folder.
type Delivery struct {
	MessageSummary
	DeliveryID int64  `json:"deliveryId"`
	Mailbox    string `json:"-"`
	Folder     string `json:"folder"`
	RcptType   string `json:"rcptType"`
	Seen       bool   `json:"seen"`
	Flagged    bool   `json:"flagged"`
}

// Folder is a mailbox folder with counts.
type Folder struct {
	Name       string  `json:"name"`
	SpecialUse *string `json:"specialUse"`
	Total      int     `json:"total"`
	Unread     int     `json:"unread"`
}

// Mailbox is a recipient address (and optionally an Inbox account).
type Mailbox struct {
	Address        string     `json:"address"`
	DisplayName    *string    `json:"displayName"`
	IsAccount      bool       `json:"isAccount"`
	Total          int        `json:"total"`
	Unread         int        `json:"unread"`
	LastReceivedAt *time.Time `json:"lastReceivedAt"`
	Folders        []Folder   `json:"folders"`
}

// Page is one page of a cursor-paginated list.
type Page[T any] struct {
	Items      []T     `json:"items"`
	Total      int     `json:"total"`
	NextCursor *string `json:"nextCursor"`
}

// DeliveryEvent is the payload of delivery.updated and delivery.deleted.
type DeliveryEvent struct {
	DeliveryID int64  `json:"deliveryId"`
	MessageID  string `json:"messageId"`
	Mailbox    string `json:"mailbox"`
	Folder     string `json:"folder"`
	Seen       bool   `json:"seen"`
	Flagged    bool   `json:"flagged"`
}

// MailboxEvent is the payload of mailbox.created.
type MailboxEvent struct {
	Address string `json:"address"`
}

// MessageRef is the payload of message.deleted.
type MessageRef struct {
	ID string `json:"id"`
}
