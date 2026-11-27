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

func TestTemplates_TryRegister_ParseError(t *testing.T) {
	tmpl := mail.NewTemplates()
	err := tmpl.TryRegister("bad", "subj", "{{")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if _, renderErr := tmpl.Render("bad", "a@b.com", nil); renderErr == nil {
		t.Fatal("failed TryRegister must not leave a registry entry")
	}
}

func TestApplyLayout_NilNoop(t *testing.T) {
	msg := mail.Message{Body: "<p>x</p>"}
	got, err := mail.ApplyLayout(nil, msg, mail.EmailData{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != msg.Body {
		t.Fatalf("body = %q", got.Body)
	}
}

func TestTemplates_PutParsed_RejectsNilAndEmpty(t *testing.T) {
	tmpl := mail.NewTemplates()
	parsed, err := mail.ParseNamed("ok", "s", "<p>x</p>")
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpl.PutParsed(map[string]*mail.NamedParts{"": parsed}); err == nil {
		t.Fatal("expected empty name error")
	}
	if err := tmpl.PutParsed(map[string]*mail.NamedParts{"ok": nil}); err == nil {
		t.Fatal("expected nil template error")
	}
	if _, err := tmpl.Render("ok", "a@b.com", nil); err == nil {
		t.Fatal("rejected PutParsed must not mutate registry")
	}
}

func TestTemplates_SourceAndNames(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("welcome", "Hi {{.Name}}", "<p>{{.Name}}</p>")
	src, ok := tmpl.Source("welcome")
	if !ok || src.Origin != mail.OriginBuiltin || src.Subject != "Hi {{.Name}}" {
		t.Fatalf("builtin source = %#v ok=%v", src, ok)
	}
	parts, err := mail.ParseNamed("order_confirmed", "File subj", "<p>file</p>")
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpl.PutFile(map[string]mail.FileEntry{
		"order_confirmed": {Parts: parts, Subject: "File subj", Body: "<p>file</p>"},
	}); err != nil {
		t.Fatal(err)
	}
	src, ok = tmpl.Source("order_confirmed")
	if !ok || src.Origin != mail.OriginFile || src.Body != "<p>file</p>" {
		t.Fatalf("file source = %#v ok=%v", src, ok)
	}
	names := tmpl.Names()
	if len(names) != 2 || names[0] != "order_confirmed" || names[1] != "welcome" {
		t.Fatalf("Names = %v", names)
	}
}

func TestTemplates_Render(t *testing.T) {
	tmpl := mail.NewTemplates()
	tmpl.Register("welcome", "Welcome {{.Name}} & Co", "<h1>Hello {{.Name}}</h1>")

	msg, err := tmpl.Render("welcome", "user@example.com", map[string]string{"Name": "Alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.To != "user@example.com" {
		t.Errorf("To = %q, want %q", msg.To, "user@example.com")
	}
	if msg.Subject != "Welcome Alice & Co" {
		t.Errorf("Subject = %q, want plain &", msg.Subject)
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
