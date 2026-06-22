package manualpay_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
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

	reg := app.PaymentRegistry()
	if reg == nil || reg.Len() != 1 {
		t.Fatalf("PaymentRegistry().Len() = %d, want 1", reg.Len())
	}
	p, err := reg.Resolve(string(payment.MethodManual))
	if err != nil {
		t.Fatalf("Resolve(manual) error: %v", err)
	}
	if p.Method() != payment.MethodManual {
		t.Fatalf("Method() = %q, want manual", p.Method())
	}
}
