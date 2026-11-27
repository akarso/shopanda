package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type claimMailerStub struct {
	last mail.Message
}

func (m *claimMailerStub) Send(_ context.Context, msg mail.Message) error {
	m.last = msg
	return nil
}

func TestStorefrontOrderClaimEmailer_RendersOrderClaim(t *testing.T) {
	dir := filepath.Join("..", "..", "templates", "emails")
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, dir, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	stub := &claimMailerStub{}
	emailer := storefrontOrderClaimEmailer{
		mailer:    stub,
		claimURL:  "https://shop.example/account/orders/claim",
		storeURL:  "https://shop.example",
		templates: tmpl,
		layout:    lr,
	}
	if err := emailer.SendClaimEmail("guest@example.com", "tok-1"); err != nil {
		t.Fatalf("SendClaimEmail: %v", err)
	}
	if stub.last.To != "guest@example.com" {
		t.Fatalf("to = %q", stub.last.To)
	}
	if stub.last.Subject != "Claim your guest order" {
		t.Fatalf("subject = %q", stub.last.Subject)
	}
	if !strings.Contains(stub.last.Body, "claim_token=tok-1") {
		t.Fatalf("body missing claim token URL: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "If you did not place an order") {
		t.Fatalf("body missing file template needle: %s", stub.last.Body)
	}
	if lr != nil && !strings.Contains(stub.last.Body, `class="container"`) {
		t.Fatalf("body missing layout wrap: %s", stub.last.Body)
	}
}
