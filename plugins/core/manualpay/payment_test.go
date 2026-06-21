package manualpay_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	inmanual "github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cmanual "github.com/akarso/shopanda/plugins/core/manualpay"
)

func testApp() *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
	}
}

func TestPaymentPlugin_Name(t *testing.T) {
	if got := cmanual.NewPaymentPlugin().Name(); got != "core/manualpay" {
		t.Fatalf("Name() = %q, want core/manualpay", got)
	}
}

func TestPaymentPlugin_Init_RegistersManualProvider(t *testing.T) {
	app := testApp()
	if err := cmanual.NewPaymentPlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	v, ok := app.PaymentProvider()
	if !ok {
		t.Fatal("PaymentProvider() ok = false, want manual provider")
	}
	prov, ok := v.(payment.Provider)
	if !ok {
		t.Fatalf("PaymentProvider() type = %T, want payment.Provider", v)
	}
	if prov.Method() != payment.MethodManual {
		t.Fatalf("Method() = %q, want manual", prov.Method())
	}
}

func TestPaymentPlugin_Init_SkipsWhenProviderAlreadySet(t *testing.T) {
	app := testApp()
	app.RegisterPaymentProvider(inmanual.NewProvider())

	if err := cmanual.NewPaymentPlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}
