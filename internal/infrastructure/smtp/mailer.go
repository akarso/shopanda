package smtp

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

// Mailer sends email via SMTP.
type Mailer struct {
	cfg Config
}

// New creates an SMTP Mailer.
func New(cfg Config) *Mailer {
	return &Mailer{cfg: cfg}
}

// Send delivers a message over SMTP.
func (m *Mailer) Send(_ context.Context, msg mail.Message) error {
	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", m.cfg.Port))

	headers := make(map[string]string)
	headers["From"] = m.cfg.From
	headers["To"] = msg.To
	headers["Subject"] = msg.Subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	var buf strings.Builder
	for k, v := range headers {
		fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
	}
	buf.WriteString("\r\n")
	buf.WriteString(msg.Body)

	var auth smtp.Auth
	if m.cfg.User != "" {
		auth = smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
	}

	return smtp.SendMail(addr, auth, m.cfg.From, []string{msg.To}, []byte(buf.String()))
}
