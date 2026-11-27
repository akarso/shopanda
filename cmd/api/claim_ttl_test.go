package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/application/notification"
	"github.com/akarso/shopanda/internal/domain/mail"
	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type claimTTLMailerStub struct {
	last mail.Message
}

func (m *claimTTLMailerStub) Send(_ context.Context, msg mail.Message) error {
	m.last = msg
	return nil
}

type claimTTLOrderRepo struct{}

func (claimTTLOrderRepo) FindByID(context.Context, string) (*domainOrder.Order, error) {
	return nil, nil
}
func (claimTTLOrderRepo) FindByIDs(context.Context, []string) (map[string]*domainOrder.Order, error) {
	return map[string]*domainOrder.Order{}, nil
}
func (claimTTLOrderRepo) FindByCustomerID(context.Context, string) ([]domainOrder.Order, error) {
	return nil, nil
}
func (claimTTLOrderRepo) FindByContactEmail(context.Context, string) ([]domainOrder.Order, error) {
	return nil, nil
}
func (claimTTLOrderRepo) List(context.Context, int, int) ([]domainOrder.Order, error) {
	return nil, nil
}
func (claimTTLOrderRepo) ListCreatedSince(context.Context, time.Time, string, int) ([]domainOrder.Order, error) {
	return nil, nil
}
func (claimTTLOrderRepo) Save(context.Context, *domainOrder.Order) error { return nil }
func (claimTTLOrderRepo) UpdateStatus(context.Context, *domainOrder.Order) error {
	return nil
}
func (claimTTLOrderRepo) LinkToCustomer(context.Context, *domainOrder.Order) error { return nil }
func (claimTTLOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (claimTTLOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]domainOrder.TaxSnapshotRow, error) {
	return nil, nil
}

type claimTTLAuther struct{}

func (claimTTLAuther) Register(context.Context, auth.RegisterInput) (auth.RegisterOutput, error) {
	return auth.RegisterOutput{}, nil
}
func (claimTTLAuther) DeleteCustomer(context.Context, string) error { return nil }

func newClaimTTLServeRuntime(t *testing.T, mailer mail.Mailer, tmpl *mail.Templates, layout mail.Layout) *serveRuntime {
	t.Helper()
	const secret = "test-secret-at-least-32-bytes-long!!"
	issuer, err := jwt.NewIssuer(secret, time.Hour)
	if err != nil {
		t.Fatalf("jwt.NewIssuer: %v", err)
	}
	return &serveRuntime{
		claimAPIOrders: claimTTLOrderRepo{},
		claimAPIAuther: claimTTLAuther{},
		jwtIssuer:      issuer,
		jwtSecretStr:   secret,
		mailer:         mailer,
		mailTemplates:  tmpl,
		emailLayout:    layout,
	}
}

func TestClaimEmailTokenTTL_ThroughAttachWiring(t *testing.T) {
	tmpl := mail.NewTemplates()
	notification.RegisterTemplates(tmpl)
	lr, err := loadFileEmailTemplates(tmpl, filepath.Join("..", "..", "templates", "emails"), logger.NewWithWriter(io.Discard, "error"))
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	if lr == nil {
		t.Fatal("expected layout")
	}

	cases := []struct {
		name       string
		publicBase string
		wantTTL    time.Duration
		wantBody   string
	}{
		{
			name:     "empty PublicBaseURL",
			wantTTL:  storefront.DefaultSecurityEmailTokenTTL,
			wantBody: "30 minutes",
		},
		{
			name:       "with PublicBaseURL",
			publicBase: "https://shop.example",
			wantTTL:    storefront.OrderClaimEmailTokenTTL,
			wantBody:   "45 minutes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.PublicBaseURL = tc.publicBase
			// Absolute claim page so SendClaimEmail can build a link without SSR mode.
			cfg.Frontend.ClaimURL = "https://shop.example/account/orders/claim"

			stub := &claimTTLMailerStub{}
			rt := newClaimTTLServeRuntime(t, stub, tmpl, lr)
			sf := attachStorefrontClaimAPI(
				storefront.NewStorefrontHandler(nil, nil, nil, nil, nil, nil),
				rt,
				cfg,
			)
			if got := sf.SecurityEmailTokenTTL(); got != tc.wantTTL {
				t.Fatalf("SecurityEmailTokenTTL = %v, want %v", got, tc.wantTTL)
			}
			// Claim + link services must be wired (not skipped) even with stub deps.
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/claim-search", strings.NewReader(url.Values{
				"contact_email": []string{"guest@example.com"},
			}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			sf.ClaimOrderSearch().ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("claim-search must be mounted (got 404); claim/link wiring was skipped")
			}
			emailer := sf.OrderClaimEmailer()
			if emailer == nil {
				t.Fatal("attachStorefrontClaimAPI must install an order claim emailer")
			}
			if err := emailer.SendClaimEmail(context.Background(), "guest@example.com", "tok-ttl"); err != nil {
				t.Fatalf("SendClaimEmail: %v", err)
			}
			if !strings.Contains(stub.last.Body, tc.wantBody) {
				t.Fatalf("body missing %q: %s", tc.wantBody, stub.last.Body)
			}
			if !strings.Contains(stub.last.Text, tc.wantBody) {
				t.Fatalf("plain text missing %q: %s", tc.wantBody, stub.last.Text)
			}
		})
	}
}

func TestFormatClaimTokenTTL(t *testing.T) {
	if formatClaimTokenTTL(storefront.DefaultSecurityEmailTokenTTL) != "30 minutes" {
		t.Fatalf("format default = %q", formatClaimTokenTTL(storefront.DefaultSecurityEmailTokenTTL))
	}
	if formatClaimTokenTTL(storefront.OrderClaimEmailTokenTTL) != "45 minutes" {
		t.Fatalf("format claim = %q", formatClaimTokenTTL(storefront.OrderClaimEmailTokenTTL))
	}
	if formatClaimTokenTTL(90*time.Second) != "1 minute" {
		t.Fatalf("format 90s = %q", formatClaimTokenTTL(90*time.Second))
	}
	// Sub-minute must not round up past ExpiresAt.
	if formatClaimTokenTTL(30*time.Second) != "30 seconds" {
		t.Fatalf("format 30s = %q, want 30 seconds", formatClaimTokenTTL(30*time.Second))
	}
	if formatClaimTokenTTL(500*time.Millisecond) != "less than 1 second" {
		t.Fatalf("format 500ms = %q, want less than 1 second", formatClaimTokenTTL(500*time.Millisecond))
	}
}
