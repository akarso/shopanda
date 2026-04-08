package mail_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
)

func TestTemplates_Register_PanicsEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty name")
		}
	}()
	tmpl := mail.NewTemplates()
	tmpl.Register("", "subj", "body")
}

func TestTemplates_Register_PanicsBadTemplate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for bad template")
		}
	}()
	tmpl := mail.NewTemplates()
	tmpl.Register("bad", "subj", "{{.Broken")
}

func TestTemplates_Render(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("welcome", "Welcome {{.Name}}", "<h1>Hello {{.Name}}</h1>")

	msg, err := tmpl.Render("welcome", "user@example.com", map[string]string{"Name": "Alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.To != "user@example.com" {
		t.Errorf("To = %q, want %q", msg.To, "user@example.com")
	}
	if msg.Subject != "Welcome Alice" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Welcome Alice")
	}
	if msg.Body != "<h1>Hello Alice</h1>" {
		t.Errorf("Body = %q, want %q", msg.Body, "<h1>Hello Alice</h1>")
	}
}

func TestTemplates_Render_UnknownTemplate(t *testing.T) {
	tmpl := mail.NewTemplates()
	_, err := tmpl.Render("missing", "a@b.com", nil)
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestMessage_Fields(t *testing.T) {
	m := mail.Message{
		To:      "test@example.com",
		Subject: "Test",
		Body:    "<p>Hello</p>",
	}
	if m.To != "test@example.com" {
		t.Errorf("To = %q", m.To)
	}
	if m.Subject != "Test" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.Body != "<p>Hello</p>" {
		t.Errorf("Body = %q", m.Body)
	}
}
