package smtp_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/mail"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
)

// TestMailer_ImplementsMailer verifies the interface is satisfied at compile time.
var _ mail.Mailer = (*smtpmail.Mailer)(nil)

func TestMailer_Send(t *testing.T) {
	// Start a minimal SMTP stub server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		fmt.Fprintf(conn, "220 localhost ESMTP\r\n")

		var dataBody strings.Builder
		inData := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					fmt.Fprintf(conn, "250 OK\r\n")
					inData = false
					continue
				}
				dataBody.WriteString(line)
				dataBody.WriteString("\n")
				continue
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				fmt.Fprintf(conn, "250 Hello\r\n")
			case strings.HasPrefix(upper, "MAIL FROM:"):
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(upper, "RCPT TO:"):
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(upper, "DATA"):
				fmt.Fprintf(conn, "354 Go ahead\r\n")
				inData = true
			case strings.HasPrefix(upper, "QUIT"):
				fmt.Fprintf(conn, "221 Bye\r\n")
				received <- dataBody.String()
				return
			default:
				fmt.Fprintf(conn, "500 Unknown\r\n")
			}
		}
	}()

	mailer := smtpmail.New(smtpmail.Config{
		Host: "127.0.0.1",
		Port: addr.Port,
		From: "shop@example.com",
	})

	msg := mail.Message{
		To:      "buyer@example.com",
		Subject: "Order Confirmed",
		Body:    "<h1>Thanks!</h1>",
	}

	if err := mailer.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var body string
	select {
	case body = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SMTP stub to deliver message")
	}
	if !strings.Contains(body, "Order Confirmed") {
		t.Errorf("expected subject in body, got:\n%s", body)
	}
	if !strings.Contains(body, "<h1>Thanks!</h1>") {
		t.Errorf("expected HTML body, got:\n%s", body)
	}
	if !strings.Contains(body, "From: shop@example.com") {
		t.Errorf("expected From header, got:\n%s", body)
	}
	if !strings.Contains(body, "To: buyer@example.com") {
		t.Errorf("expected To header, got:\n%s", body)
	}
}

func TestMailer_Send_RejectsCRLF(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		msg         mail.Message
		expectedErr string
	}{
		{"newline in To", "ok@example.com", mail.Message{To: "a@b.com\nBcc: evil@x.com", Subject: "ok", Body: "ok"}, "invalid To"},
		{"CR in Subject", "ok@example.com", mail.Message{To: "a@b.com", Subject: "ok\r\nBcc: evil@x.com", Body: "ok"}, "invalid Subject"},
		{"CRLF in From", "ok@example.com\r\nBcc:evil@x.com", mail.Message{To: "a@b.com", Subject: "ok", Body: "ok"}, "invalid From"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := smtpmail.New(smtpmail.Config{Host: "127.0.0.1", Port: 2525, From: tt.from})
			err := m.Send(context.Background(), tt.msg)
			if err == nil {
				t.Fatal("expected error for CRLF injection")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Fatalf("error %q does not contain %q", err, tt.expectedErr)
			}
		})
	}
}

func TestMailer_SendWithAttachment(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		fmt.Fprintf(conn, "220 localhost ESMTP\r\n")

		var dataBody strings.Builder
		inData := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					fmt.Fprintf(conn, "250 OK\r\n")
					inData = false
					continue
				}
				dataBody.WriteString(line)
				dataBody.WriteString("\n")
				continue
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				fmt.Fprintf(conn, "250 Hello\r\n")
			case strings.HasPrefix(upper, "MAIL FROM:"):
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(upper, "RCPT TO:"):
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(upper, "DATA"):
				fmt.Fprintf(conn, "354 Go ahead\r\n")
				inData = true
			case strings.HasPrefix(upper, "QUIT"):
				fmt.Fprintf(conn, "221 Bye\r\n")
				received <- dataBody.String()
				return
			default:
				fmt.Fprintf(conn, "500 Unknown\r\n")
			}
		}
	}()

	mailer := smtpmail.New(smtpmail.Config{
		Host: "127.0.0.1",
		Port: addr.Port,
		From: "shop@example.com",
	})

	msg := mail.Message{
		To:      "buyer@example.com",
		Subject: "Your Invoice",
		Body:    "<h1>Invoice attached</h1>",
		Attachments: []mail.Attachment{
			{
				Filename:    "invoice-42.pdf",
				ContentType: "application/pdf",
				Data:        []byte("%PDF-test-data"),
			},
		},
	}

	if err := mailer.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var body string
	select {
	case body = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SMTP stub")
	}

	if !strings.Contains(body, "multipart/mixed") {
		t.Errorf("expected multipart/mixed content type, got:\n%s", body)
	}
	if !strings.Contains(body, "<h1>Invoice attached</h1>") {
		t.Errorf("expected HTML body part, got:\n%s", body)
	}
	if !strings.Contains(body, "Content-Disposition: attachment") {
		t.Errorf("expected attachment disposition, got:\n%s", body)
	}
	if !strings.Contains(body, "application/pdf") {
		t.Errorf("expected application/pdf content type, got:\n%s", body)
	}
}
