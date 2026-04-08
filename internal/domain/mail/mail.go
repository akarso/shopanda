package mail

import "context"

// Message represents an email to be sent.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends email messages.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
