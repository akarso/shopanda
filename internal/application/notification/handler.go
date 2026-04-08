package notification

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
)

// EmailSendHandler processes email.send jobs by sending the email via Mailer.
type EmailSendHandler struct {
	mailer mail.Mailer
}

// NewEmailSendHandler creates a handler for email.send jobs.
func NewEmailSendHandler(mailer mail.Mailer) *EmailSendHandler {
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

	return h.mailer.Send(ctx, msg)
}
