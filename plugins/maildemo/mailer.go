package maildemo

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const (
	// DefaultSubjectPrefix is applied when config subject_prefix is empty.
	DefaultSubjectPrefix = "[maildemo]"
)

// LogMailer implements mail.Mailer by logging outbound messages.
// Reference integrators can swap this for SendGrid/Postmark HTTP clients.
type LogMailer struct {
	log           logger.Logger
	subjectPrefix string
}

// NewLogMailer creates a mail sender that logs instead of delivering externally.
func NewLogMailer(log logger.Logger, subjectPrefix string) *LogMailer {
	prefix := strings.TrimSpace(subjectPrefix)
	if prefix == "" {
		prefix = DefaultSubjectPrefix
	}
	return &LogMailer{log: log, subjectPrefix: prefix}
}

// Send logs the message and returns nil. Attachments are counted but not logged.
func (m *LogMailer) Send(_ context.Context, msg mail.Message) error {
	if m == nil || m.log == nil {
		return fmt.Errorf("maildemo: logger not configured")
	}
	to := strings.TrimSpace(msg.To)
	if to == "" {
		return fmt.Errorf("maildemo: recipient must not be empty")
	}
	subject := strings.TrimSpace(msg.Subject)
	if subject != "" && !strings.HasPrefix(subject, m.subjectPrefix) {
		subject = m.subjectPrefix + " " + subject
	}
	m.log.Info("maildemo.send", map[string]interface{}{
		"to":               to,
		"subject":          subject,
		"body_bytes":       len(msg.Body),
		"attachment_count": len(msg.Attachments),
	})
	return nil
}
