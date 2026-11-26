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
	wrapped, err := mail.ApplyLayout(lr, msg, ed)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !strings.Contains(wrapped.Body, "file-body-ord-9") {
		t.Fatalf("wrapped missing file body: %s", wrapped.Body)
	}
	if !strings.Contains(wrapped.Body, "<footer>Acme</footer>") {
		t.Fatalf("wrapped missing layout chrome: %s", wrapped.Body)
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

func TestLoadFileEmailTemplates_ParseErrorFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.html"), []byte("<!-- Subject: X -->\n{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	_, err := loadFileEmailTemplates(tmpl, dir, logger.NewWithWriter(io.Discard, "error"))
	if err == nil || !strings.Contains(err.Error(), "parse template broken") {
		t.Fatalf("err = %v, want parse template broken", err)
	}
}

func TestLoadFileEmailTemplates_NotDirFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl := mail.NewTemplates()
	_, err := loadFileEmailTemplates(tmpl, path, logger.NewWithWriter(io.Discard, "error"))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want not a directory", err)
	}
}

func TestLoadFileEmailTemplates_RepoTree(t *testing.T) {
	dir := filepath.Join("..", "..", "templates", "emails")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("repo templates dir unavailable: %v", err)
	}
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, dir, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load repo tree: %v", err)
	}
	if lr == nil {
		t.Fatal("expected layout from repo tree")
	}

	samples := map[string]mail.EmailData{
		"order_confirmed": {
			StoreName: "Acme",
			StoreURL:  "https://shop.example",
			Data: map[string]interface{}{
				"OrderID":   "ord-1",
				"FirstName": "Ada",
				"Items":     []map[string]interface{}{{"Name": "Tee", "Qty": 1, "Price": "EUR 10.00"}},
			},
		},
		"password_reset": {
			StoreName: "Acme",
			Data:      map[string]interface{}{"ResetURL": "https://shop.example/reset"},
		},
		"order_shipped": {
			StoreName: "Acme",
			StoreURL:  "https://shop.example",
			Data: map[string]interface{}{
				"OrderID":        "ord-1",
				"TrackingNumber": "TRACK-1",
			},
		},
		"invoice_created": {
			StoreName: "Acme",
			StoreURL:  "https://shop.example",
			Data: map[string]interface{}{
				"InvoiceNumber": int64(42),
				"OrderID":       "ord-1",
			},
		},
	}
	for name, ed := range samples {
		msg, renderErr := tmpl.Render(name, "a@example.com", ed)
		if renderErr != nil {
			t.Fatalf("%s render: %v", name, renderErr)
		}
		if strings.TrimSpace(msg.Subject) == "" {
			t.Fatalf("%s empty subject", name)
		}
		wrapped, wrapErr := mail.ApplyLayout(lr, msg, ed)
		if wrapErr != nil {
			t.Fatalf("%s wrap: %v", name, wrapErr)
		}
		if !strings.Contains(wrapped.Body, `class="container"`) {
			t.Fatalf("%s missing layout wrapper: %s", name, wrapped.Body)
		}
		if strings.Count(wrapped.Body, `class="container"`) != 1 {
			t.Fatalf("%s expected single layout wrapper", name)
		}
	}

	builtin := mail.EmailData{StoreName: "Acme", Data: map[string]interface{}{"Code": "123456"}}
	msg, err := tmpl.Render("email_verification", "a@example.com", builtin)
	if err != nil {
		t.Fatalf("builtin email_verification: %v", err)
	}
	wrapped, err := mail.ApplyLayout(lr, msg, builtin)
	if err != nil {
		t.Fatalf("wrap builtin: %v", err)
	}
	if !strings.Contains(wrapped.Body, `class="container"`) {
		t.Fatalf("builtin not wrapped: %s", wrapped.Body)
	}
}
