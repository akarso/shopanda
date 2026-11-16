package extapi

import "context"

// MailAttachment is an email file attachment.
type MailAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// MailMessage is an email to be sent.
type MailMessage struct {
	To          string
	Subject     string
	Body        string
	Attachments []MailAttachment
}

// MailSender sends email messages. Register via Host.RegisterMailSender.
type MailSender interface {
	Send(ctx context.Context, msg MailMessage) error
}
