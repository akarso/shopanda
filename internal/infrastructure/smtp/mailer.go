package smtp

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
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

// Send delivers a message over SMTP. The context controls connection
// establishment and, when it carries a deadline, sets a write deadline on the
// underlying TCP connection.
func (m *Mailer) Send(ctx context.Context, msg mail.Message) error {
	if err := rejectCRLF(m.cfg.From); err != nil {
		return fmt.Errorf("smtp: invalid From: %w", err)
	}
	if err := rejectCRLF(msg.To); err != nil {
		return fmt.Errorf("smtp: invalid To: %w", err)
	}
	if err := rejectCRLF(msg.Subject); err != nil {
		return fmt.Errorf("smtp: invalid Subject: %w", err)
	}

	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprintf("%d", m.cfg.Port))

	// Context-aware TCP connection.
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}

	// Propagate context deadline to the connection.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: new client: %w", err)
	}
	defer client.Close()

	if m.cfg.User != "" {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "From: %s\r\n", m.cfg.From)
	fmt.Fprintf(&buf, "To: %s\r\n", msg.To)
	fmt.Fprintf(&buf, "Subject: %s\r\n", msg.Subject)
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(msg.Body)
	} else {
		boundary := "==shopanda_boundary=="
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
		buf.WriteString("\r\n")

		// HTML body part.
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(msg.Body)
		buf.WriteString("\r\n")

		// Attachment parts.
		for _, att := range msg.Attachments {
			fmt.Fprintf(&buf, "--%s\r\n", boundary)
			fmt.Fprintf(&buf, "Content-Type: %s\r\n", att.ContentType)
			fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%s\r\n",
				mime.QEncoding.Encode("utf-8", att.Filename))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString("\r\n")
			buf.WriteString(base64.StdEncoding.EncodeToString(att.Data))
			buf.WriteString("\r\n")
		}
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	}

	if _, err := wc.Write([]byte(buf.String())); err != nil {
		wc.Close()
		return fmt.Errorf("smtp: write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp: close data: %w", err)
	}

	return client.Quit()
}

// rejectCRLF returns an error if s contains CR or LF characters,
// preventing SMTP header injection.
func rejectCRLF(s string) error {
	if strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("contains CR or LF")
	}
	return nil
}
