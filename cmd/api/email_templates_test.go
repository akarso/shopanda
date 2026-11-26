package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestLoadFileEmailTemplates_OverwritesBuiltin(t *testing.T) {
	dir := t.TempDir()
	body := "<!-- Subject: FILE-SUBJECT {{.Data.OrderID}} -->\n<p>file-body-{{.Data.OrderID}}</p>\n"
	if err := os.WriteFile(filepath.Join(dir, "order_confirmed.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	layout := `<html><body>{{.Body}}<footer>{{.StoreName}}</footer></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "_layout.html"), []byte(layout), 0o644); err != nil {
		t.Fatal(err)
	}

	log := logger.NewWithWriter(io.Discard, "error")
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, dir, log)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lr == nil {
		t.Fatal("expected layout renderer")
	}

	ed := mail.EmailData{
		StoreName: "Acme",
		Data:      map[string]interface{}{"OrderID": "ord-9"},
	}
	msg, err := tmpl.Render("order_confirmed", "a@example.com", ed)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "FILE-SUBJECT ord-9" {
		t.Fatalf("subject = %q, want file override", msg.Subject)
	}
	wrapped, err := lr.Wrap(msg.Body, ed)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !strings.Contains(wrapped, "file-body-ord-9") {
		t.Fatalf("wrapped missing file body: %s", wrapped)
	}
	if !strings.Contains(wrapped, "<footer>Acme</footer>") {
		t.Fatalf("wrapped missing layout chrome: %s", wrapped)
	}
}

func TestLoadFileEmailTemplates_MissingDirKeepsBuiltins(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, filepath.Join(t.TempDir(), "nope"), logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lr != nil {
		t.Fatal("expected nil layout when dir missing")
	}
	msg, err := tmpl.Render("order_confirmed", "a@example.com", mail.EmailData{
		Data: map[string]interface{}{"OrderID": "ord-1"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(msg.Subject, "Confirmation") {
		t.Fatalf("builtin subject missing: %q", msg.Subject)
	}
}
