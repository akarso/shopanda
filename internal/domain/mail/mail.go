package mail

import (
	"context"
	"errors"
)

// ErrTemplateNotFound is returned when a template cannot be resolved.
var ErrTemplateNotFound = errors.New("mail: template not found")

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
	Body        string
	Attachments []Attachment
}

// Mailer sends email messages.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
