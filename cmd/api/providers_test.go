package main

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
)

func TestResolvePaymentProvider_FromPlugin(t *testing.T) {
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

	prov, err := resolvePaymentProvider(app)
	if err != nil {
		t.Fatalf("resolvePaymentProvider() error: %v", err)
	}
	if prov.Method() != payment.MethodManual {
		t.Fatalf("Method() = %q, want manual", prov.Method())
	}
}

func TestResolvePaymentProvider_StripeWinsWhenConfigured(t *testing.T) {
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

	prov, err := resolvePaymentProvider(app)
	if err != nil {
		t.Fatalf("resolvePaymentProvider() error: %v", err)
	}
	if prov.Method() != payment.MethodStripe {
		t.Fatalf("Method() = %q, want stripe", prov.Method())
	}
}
