package mail

import (
	"context"
	"errors"
)

// ErrTemplateNotFound is returned when a template cannot be resolved.
var ErrTemplateNotFound = errors.New("mail: template not found")

// ErrDial means the message was almost certainly not accepted: the transport
// failed before SMTP DATA (e.g. TCP dial). Claim-mail retry matches this with
// errors.Is. Mailers (SMTP or plugin) should wrap dial/connect failures:
//
//	return fmt.Errorf("%w: %w", mail.ErrDial, err)
//
// Do not wrap post-dial SMTP replies with ErrDial.
var ErrDial = errors.New("mail: dial")

// Attachment represents an email file attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Message represents an email to be sent.
type Message struct {
	To          string
	Subject     string
	Body        string // HTML body
	Text        string // optional plain-text alternative (multipart/alternative)
	Attachments []Attachment
}

// Mailer sends email messages.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
