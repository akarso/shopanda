package main

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/maildemo"
)

func TestResolvePaymentRegistry_FromPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	payReg, err := resolvePaymentRegistry(app)
	if err != nil {
		t.Fatalf("resolvePaymentRegistry() error: %v", err)
	}
	if payReg.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 manual provider", payReg.Len())
	}
	p, err := payReg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if p.Method() != payment.MethodManual {
		t.Fatalf("Method() = %q, want manual", p.Method())
	}
}

func TestResolvePaymentRegistry_ManualAndStripe(t *testing.T) {
	t.Setenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY", "sk_test_example")

	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	payReg, err := resolvePaymentRegistry(app)
	if err != nil {
		t.Fatalf("resolvePaymentRegistry() error: %v", err)
	}
	if payReg.Len() != 2 {
		t.Fatalf("Len() = %d, want manual and stripe", payReg.Len())
	}

	stripe, err := payReg.Resolve(string(payment.MethodStripe))
	if err != nil {
		t.Fatalf("Resolve(stripe) error: %v", err)
	}
	if stripe.Method() != payment.MethodStripe {
		t.Fatalf("stripe Method() = %q", stripe.Method())
	}
}

func TestResolveMediaStorage_FromLocalPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "local",
			Local: config.LocalStorageConfig{
				BasePath: "./public/media",
				BaseURL:  "/media",
			},
		},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	st, err := resolveMediaStorage(app, cfg)
	if err != nil {
		t.Fatalf("resolveMediaStorage() error: %v", err)
	}
	if st == nil {
		t.Fatal("resolveMediaStorage() returned nil storage")
	}
}

func TestResolveMailer_FromPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			MailDemo: config.MailDemoPluginConfig{Enabled: true},
		},
		Mail: config.MailConfig{Driver: "smtp"},
	}
	reg.Register(maildemo.New())
	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: cfg}
	if summary := reg.InitAll(app); summary.Failed != 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}

	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		t.Fatalf("resolveMailer() error: %v", err)
	}
	if err := mailer.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Test",
		Body:    "Hello",
	}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
}

func TestResolveMailer_CoreSMTPDefault(t *testing.T) {
	app := &plugin.App{}
	cfg := &config.Config{
		Mail: config.MailConfig{
			Driver: "smtp",
			SMTP:   config.SMTPConfig{Host: "localhost", Port: 25, From: "shop@localhost"},
		},
	}
	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		t.Fatalf("resolveMailer() error: %v", err)
	}
	if mailer == nil {
		t.Fatal("resolveMailer() returned nil")
	}
}
