package pluginsdk

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AdaptMailSender wraps a public MailSender as a core mail.Mailer for
// registration via Host.RegisterMailSender.
func AdaptMailSender(sender extapi.MailSender) mail.Mailer {
	if sender == nil {
		panic("pluginsdk: AdaptMailSender: sender must not be nil")
	}
	return &mailSenderBridge{sender: sender}
}

type mailSenderBridge struct {
	sender extapi.MailSender
}

func (b *mailSenderBridge) Send(ctx context.Context, msg mail.Message) error {
	attachments := make([]extapi.MailAttachment, len(msg.Attachments))
	for i, a := range msg.Attachments {
		attachments[i] = extapi.MailAttachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Data:        a.Data,
		}
	}
	return b.sender.Send(ctx, extapi.MailMessage{
		To:          msg.To,
		Subject:     msg.Subject,
		Body:        msg.Body,
		Attachments: attachments,
	})
}
