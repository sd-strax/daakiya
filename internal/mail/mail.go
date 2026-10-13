// Package mail defines the mailbox abstraction the pipeline works against.
// internal/mail/graph implements it for Microsoft Graph.
package mail

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("message not found")

type Address struct {
	Name    string
	Address string
}

// Message is the subset of a mail message the pipeline needs. Body and Headers
// are only populated by Fetch, not by delta listing.
type Message struct {
	ID                string
	InternetMessageID string
	ConversationID    string
	ReceivedAt        time.Time
	From              Address
	Sender            Address
	ReplyTo           []Address
	To                []Address
	CC                []Address
	Subject           string
	BodyPreview       string
	BodyText          string // populated by Fetch
	BodyHTML          string // populated by Fetch when available
	Headers           map[string][]string
	HasAttachments    bool
	IsDraft           bool
	WebLink           string
	ParentFolderID    string
}

// Folders are the IDs the pipeline needs after bootstrap.
type Folders struct {
	InboxID  string
	FolderID string            // parent "Daakiya"
	Sub      map[string]string // subfolder name -> id
}

// Delta is the result of one delta sync.
type Delta struct {
	New     []Message
	Removed []string // IDs that left the Inbox since the cursor
	Cursor  string   // opaque, store for next time
}

type Mailbox interface {
	// WhoAmI returns the signed-in user's principal name and display name.
	WhoAmI(ctx context.Context) (upn, name string, err error)
	// Bootstrap ensures the parent folder, each subfolder and the categories exist.
	Bootstrap(ctx context.Context, folder string, subfolders []string, categories []string) (Folders, error)
	// InitialCursor returns a cursor representing "now" without listing messages.
	InitialCursor(ctx context.Context) (string, error)
	// NewMessages lists Inbox changes since cursor.
	NewMessages(ctx context.Context, cursor string) (Delta, error)
	// Fetch loads the body and headers for one message.
	Fetch(ctx context.Context, id string) (Message, error)
	// InboxCount returns the total number of messages in the Inbox.
	InboxCount(ctx context.Context) (int, error)
	// ListInboxSince lists Inbox messages received after since (for backfill).
	ListInboxSince(ctx context.Context, since time.Time, max int) ([]Message, error)
	// ListFolder lists messages currently in a folder.
	ListFolder(ctx context.Context, folderID string, max int) ([]Message, error)
	Move(ctx context.Context, id, destFolderID string) (newID string, err error)
	SetCategories(ctx context.Context, id string, categories []string) error
	GetCategories(ctx context.Context, id string) ([]string, error)
	// Locate returns the folder a message is in now.
	Locate(ctx context.Context, id string) (folderID string, err error)
	// KnownCorrespondents scans Sent Items for recipients and conversation IDs.
	KnownCorrespondents(ctx context.Context, since time.Time, maxMessages int) (addresses, conversationIDs []string, err error)
}
