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

func TestLoadFileEmailTemplates_EmptyAbsKeepsBuiltins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "_layout.html"), []byte("<html>{{.Body}}</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, dir, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lr != nil {
		t.Fatal("empty body dir must not load layout as success")
	}
	msg, err := tmpl.Render("order_confirmed", "a@example.com", mail.EmailData{
		Data: map[string]interface{}{"OrderID": "ord-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Subject, "Confirmation") {
		t.Fatalf("builtin subject missing: %q", msg.Subject)
	}
}

func TestLoadFileEmailTemplates_EmptyRelativeFallsThrough(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty", "templates", "emails")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, "full")
	if err := os.MkdirAll(filepath.Join(full, "templates", "emails"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "<!-- Subject: FALLTHROUGH {{.Data.OrderID}} -->\n<p>fallthrough-body</p>\n"
	if err := os.WriteFile(filepath.Join(full, "templates", "emails", "order_confirmed.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "templates", "emails", "_layout.html"), []byte("<html>{{.Body}}</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := emailTemplatesFallbackRoots
	emailTemplatesFallbackRoots = []string{full}
	defer func() { emailTemplatesFallbackRoots = prev }()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(root, "empty")); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, defaultEmailTemplatesDir, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lr == nil {
		t.Fatal("expected layout from fallback root")
	}
	msg, err := tmpl.Render("order_confirmed", "a@example.com", mail.EmailData{
		Data: map[string]interface{}{"OrderID": "ord-9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "FALLTHROUGH ord-9" {
		t.Fatalf("subject = %q, want fallthrough override", msg.Subject)
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
	fileOnly := map[string]string{
		"order_confirmed": "We'll notify you when your order ships.",
		"order_shipped":   "Your order is on its way!",
		"password_reset":  "Reset Password",
		"invoice_created": "is attached as a PDF",
	}
	for name, ed := range samples {
		msg, renderErr := tmpl.Render(name, "a@example.com", ed)
		if renderErr != nil {
			t.Fatalf("%s render: %v", name, renderErr)
		}
		if strings.TrimSpace(msg.Subject) == "" {
			t.Fatalf("%s empty subject", name)
		}
		if needle := fileOnly[name]; needle != "" && !strings.Contains(msg.Body, needle) {
			t.Fatalf("%s missing file-only body %q (LoadDir may not have overwritten)\n%s", name, needle, msg.Body)
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
