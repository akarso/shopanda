package stripe_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cstripe "github.com/akarso/shopanda/plugins/core/stripe"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestPaymentPlugin_Name(t *testing.T) {
	if got := cstripe.NewPaymentPlugin().Name(); got != "core/stripe" {
		t.Fatalf("Name() = %q, want core/stripe", got)
	}
}

func TestPaymentPlugin_Init_Disabled(t *testing.T) {
	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: false},
		},
	}
	app := testApp(cfg)
	if err := cstripe.NewPaymentPlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if _, ok := app.PaymentProvider(); ok {
		t.Fatal("PaymentProvider() should not be set when Stripe is disabled")
	}
}

func TestPaymentPlugin_Init_EnabledWithoutKey(t *testing.T) {
	t.Setenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY", "")

	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	app := testApp(cfg)
	if err := cstripe.NewPaymentPlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if _, ok := app.PaymentProvider(); ok {
		t.Fatal("PaymentProvider() should not be set without secret key")
	}
}

func TestPaymentPlugin_Init_RegistersStripeProvider(t *testing.T) {
	t.Setenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY", "sk_test_example")

	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	app := testApp(cfg)
	if err := cstripe.NewPaymentPlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	v, ok := app.PaymentProvider()
	if !ok {
		t.Fatal("PaymentProvider() ok = false, want stripe provider")
	}
	prov, ok := v.(payment.Provider)
	if !ok {
		t.Fatalf("PaymentProvider() type = %T, want payment.Provider", v)
	}
	if prov.Method() != payment.MethodStripe {
		t.Fatalf("Method() = %q, want stripe", prov.Method())
	}
}
