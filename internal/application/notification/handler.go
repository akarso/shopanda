package notification

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
)

// EmailSendHandler processes email.send jobs by sending the email via Mailer.
type EmailSendHandler struct {
	mailer mail.Mailer
}

// NewEmailSendHandler creates a handler for email.send jobs.
// Panics if mailer is nil.
func NewEmailSendHandler(mailer mail.Mailer) *EmailSendHandler {
	if mailer == nil {
		panic("NewEmailSendHandler: nil mailer")
	}
	return &EmailSendHandler{mailer: mailer}
}

// Type returns the job type this handler processes.
func (h *EmailSendHandler) Type() string { return JobTypeEmailSend }

// Handle sends the email described in the job payload.
func (h *EmailSendHandler) Handle(ctx context.Context, job jobs.Job) error {
	to, _ := job.Payload["to"].(string)
	subject, _ := job.Payload["subject"].(string)
	body, _ := job.Payload["body"].(string)

	if to == "" {
		return fmt.Errorf("email.send: missing 'to' in payload")
	}

	msg := mail.Message{
		To:      to,
		Subject: subject,
		Body:    body,
	}

	if attsRaw, ok := job.Payload["attachments"].([]interface{}); ok {
		for _, raw := range attsRaw {
			att, _ := raw.(map[string]interface{})
			if att == nil {
				continue
			}
			filename, _ := att["filename"].(string)
			contentType, _ := att["content_type"].(string)
			dataStr, _ := att["data"].(string)
			data, err := base64.StdEncoding.DecodeString(dataStr)
			if err != nil {
				return fmt.Errorf("email.send: decode attachment %q: %w", filename, err)
			}
			msg.Attachments = append(msg.Attachments, mail.Attachment{
				Filename:    filename,
				ContentType: contentType,
				Data:        data,
			})
		}
	}

	return h.mailer.Send(ctx, msg)
}
