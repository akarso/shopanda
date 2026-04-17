package email

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// --- FileLoader tests ---

func TestFileLoader_Load(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "welcome.html",
		"<!-- Subject: Welcome {{.Data.Name}} -->\n<h1>Hello!</h1>")

	loader := NewFileLoader(dir)
	subject, body, err := loader.Load("welcome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "Welcome {{.Data.Name}}" {
		t.Errorf("subject = %q, want %q", subject, "Welcome {{.Data.Name}}")
	}
	if body != "<h1>Hello!</h1>" {
		t.Errorf("body = %q, want %q", body, "<h1>Hello!</h1>")
	}
}

func TestFileLoader_Load_NotFound(t *testing.T) {
	dir := t.TempDir()
	loader := NewFileLoader(dir)
	_, _, err := loader.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !errors.Is(err, mail.ErrTemplateNotFound) {
		t.Errorf("error = %v, want ErrTemplateNotFound", err)
	}
}

func TestFileLoader_Load_MissingSubject(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "nosubject.html", "<h1>No subject line</h1>")

	loader := NewFileLoader(dir)
	_, _, err := loader.Load("nosubject")
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
	if !strings.Contains(err.Error(), "missing subject line") {
		t.Errorf("error = %v, want 'missing subject line'", err)
	}
}

func TestFileLoader_Load_SubjectWithSpaces(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "spaced.html",
		"<!-- Subject:   Order #123 — Shipped   -->\n<p>Shipped!</p>")

	loader := NewFileLoader(dir)
	subject, _, err := loader.Load("spaced")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "Order #123 — Shipped" {
		t.Errorf("subject = %q, want %q", subject, "Order #123 — Shipped")
	}
}

// --- LayoutRenderer tests ---

func TestLayoutRenderer_Wrap(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "_layout.html",
		"<html><body>{{.Body}}<footer>{{.StoreName}}</footer></body></html>")

	lr, err := NewLayoutRenderer(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ed := mail.EmailData{StoreName: "TestShop"}
	result, err := lr.Wrap("<p>Content</p>", ed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "<p>Content</p>") {
		t.Error("wrapped output should contain the body")
	}
	if !strings.Contains(result, "TestShop") {
		t.Error("wrapped output should contain the store name")
	}
}

func TestLayoutRenderer_Wrap_NoLayout(t *testing.T) {
	dir := t.TempDir() // no _layout.html

	lr, err := NewLayoutRenderer(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := "<p>Unwrapped content</p>"
	result, err := lr.Wrap(body, mail.EmailData{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != body {
		t.Errorf("body = %q, want %q (unchanged)", result, body)
	}
}

// --- LoadDir tests ---

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "order_confirmed.html",
		"<!-- Subject: Order {{.OrderID}} -->\n<p>Thanks!</p>")
	writeTemplate(t, dir, "password_reset.html",
		"<!-- Subject: Reset your password -->\n<p>Click here</p>")
	writeTemplate(t, dir, "_layout.html",
		"<html>{{.Body}}</html>") // should be skipped

	registry := mail.NewTemplates()
	loader := NewFileLoader(dir)

	if err := LoadDir(registry, loader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both templates were registered
	msg, err := registry.Render("order_confirmed", "test@example.com", map[string]string{"OrderID": "ORD-1"})
	if err != nil {
		t.Fatalf("render order_confirmed: %v", err)
	}
	if msg.Subject != "Order ORD-1" {
		t.Errorf("subject = %q, want %q", msg.Subject, "Order ORD-1")
	}

	msg2, err := registry.Render("password_reset", "test@example.com", nil)
	if err != nil {
		t.Fatalf("render password_reset: %v", err)
	}
	if msg2.Subject != "Reset your password" {
		t.Errorf("subject = %q, want %q", msg2.Subject, "Reset your password")
	}
}

func TestLoadDir_SkipsNonHTML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "readme.txt", "not a template")
	writeTemplate(t, dir, "order.html",
		"<!-- Subject: Order -->\n<p>Order</p>")

	registry := mail.NewTemplates()
	loader := NewFileLoader(dir)

	if err := LoadDir(registry, loader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// order should be registered, readme.txt ignored
	if _, err := registry.Render("order", "x@x.com", nil); err != nil {
		t.Errorf("order template should exist: %v", err)
	}
}

// --- RenderEmail tests ---

func TestRenderEmail_WithLayout(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "_layout.html",
		"<html><body>{{.Body}}<footer>{{.StoreName}}</footer></body></html>")

	lr, err := NewLayoutRenderer(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	registry := mail.NewTemplates()
	registry.Register("test", "Test Subject", "<p>Hello {{.Data.Name}}</p>")

	data := mail.EmailData{
		StoreName: "MyStore",
		Data:      map[string]interface{}{"Name": "Alice"},
	}
	msg, err := RenderEmail(registry, lr, "test", "alice@example.com", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.To != "alice@example.com" {
		t.Errorf("To = %q, want alice@example.com", msg.To)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want 'Test Subject'", msg.Subject)
	}
	if !strings.Contains(msg.Body, "Hello Alice") {
		t.Error("body should contain rendered template content")
	}
	if !strings.Contains(msg.Body, "MyStore") {
		t.Error("body should contain store name from layout")
	}
}

func TestRenderEmail_NilLayout(t *testing.T) {
	registry := mail.NewTemplates()
	registry.Register("plain", "Subject", "<p>body</p>")

	msg, err := RenderEmail(registry, nil, "plain", "x@x.com", mail.EmailData{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Body != "<p>body</p>" {
		t.Errorf("body = %q, want <p>body</p>", msg.Body)
	}
}

// --- Setup tests ---

func TestSetup(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "_layout.html", "<html>{{.Body}}</html>")

	loader, lr, absDir, err := Setup(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loader == nil {
		t.Error("loader should not be nil")
	}
	if lr == nil {
		t.Error("layout renderer should not be nil")
	}
	if absDir == "" {
		t.Error("absDir should not be empty")
	}
}

func TestSetup_MissingDir(t *testing.T) {
	_, _, _, err := Setup(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestLoadDir_OverwritesPreviousRegistration(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "override_me.html",
		"<!-- Subject: File Subject -->\n<p>File Body</p>")

	registry := mail.NewTemplates()
	registry.Register("override_me", "Hardcoded Subject", "<p>Hardcoded Body</p>")

	loader := NewFileLoader(dir)
	if err := LoadDir(registry, loader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := registry.Render("override_me", "x@x.com", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "File Subject" {
		t.Errorf("subject = %q, want %q (file should override hardcoded)", msg.Subject, "File Subject")
	}
	if msg.Body != "<p>File Body</p>" {
		t.Errorf("body = %q, want %q (file should override hardcoded)", msg.Body, "<p>File Body</p>")
	}
}

// --- helpers ---

func writeTemplate(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
