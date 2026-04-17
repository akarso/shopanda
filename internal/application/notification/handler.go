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

	if err := parseAttachments(job.Payload, &msg); err != nil {
		return err
	}

	return h.mailer.Send(ctx, msg)
}

// parseAttachments extracts and validates attachments from a job payload.
func parseAttachments(payload map[string]interface{}, msg *mail.Message) error {
	raw, ok := payload["attachments"]
	if !ok {
		return nil
	}

	var entries []map[string]interface{}
	switch v := raw.(type) {
	case []interface{}:
		for i, elem := range v {
			m, ok := elem.(map[string]interface{})
			if !ok {
				return fmt.Errorf("email.send: attachment[%d] is not a map", i)
			}
			entries = append(entries, m)
		}
	case []map[string]interface{}:
		entries = v
	default:
		return fmt.Errorf("email.send: attachments has unexpected type %T", raw)
	}

	for i, att := range entries {
		filename, _ := att["filename"].(string)
		contentType, _ := att["content_type"].(string)
		dataStr, _ := att["data"].(string)

		if filename == "" {
			return fmt.Errorf("email.send: attachment[%d] missing filename", i)
		}
		if contentType == "" {
			return fmt.Errorf("email.send: attachment[%d] %q missing content_type", i, filename)
		}
		if dataStr == "" {
			return fmt.Errorf("email.send: attachment[%d] %q missing data", i, filename)
		}

		data, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			return fmt.Errorf("email.send: attachment[%d] %q decode failed: %w", i, filename, err)
		}

		msg.Attachments = append(msg.Attachments, mail.Attachment{
			Filename:    filename,
			ContentType: contentType,
			Data:        data,
		})
	}
	return nil
}
