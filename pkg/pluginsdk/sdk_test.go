package pluginsdk_test

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func testApp() *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
	}
}

func TestAfterBeforePricingPosition(t *testing.T) {
	if got := string(pluginsdk.After("promotions")); got != "after:promotions" {
		t.Fatalf("After() = %q", got)
	}
	if got := string(pluginsdk.Before("tax")); got != "before:tax" {
		t.Fatalf("Before() = %q", got)
	}
	if got := string(pluginsdk.Replace("taxes")); got != "replace:taxes" {
		t.Fatalf("Replace() = %q", got)
	}
}

func TestCheckoutPositionHelpers(t *testing.T) {
	if got := string(pluginsdk.CheckoutStart()); got != "start" {
		t.Fatalf("CheckoutStart() = %q", got)
	}
	if got := string(pluginsdk.CheckoutEnd()); got != "end" {
		t.Fatalf("CheckoutEnd() = %q", got)
	}
	if got := string(pluginsdk.CheckoutBefore("order")); got != "before:order" {
		t.Fatalf("CheckoutBefore() = %q", got)
	}
}

func TestCheckout_Register(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "acme/demo")
	sdk.Checkout().Register("step-a")
	sdk.Checkout().Register("step-b", pluginsdk.CheckoutBefore("create_order"))

	regs := app.CheckoutStepRegistrations()
	if len(regs) != 2 {
		t.Fatalf("registrations = %+v", regs)
	}
	if regs[0].Step != "step-a" || regs[0].Position != "end" {
		t.Fatalf("first = %+v", regs[0])
	}
	if regs[1].Step != "step-b" || regs[1].Position != "before:create_order" {
		t.Fatalf("second = %+v", regs[1])
	}
}

func TestPricing_Register(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "acme/demo")
	sdk.Pricing().Register("step-a")
	sdk.Pricing().Register("step-b", pluginsdk.After("promotions"))

	regs := app.PricingStepRegistrations()
	if len(regs) != 2 {
		t.Fatalf("registrations = %+v", regs)
	}
	if regs[0].Step != "step-a" || regs[0].Position != "after:base" {
		t.Fatalf("first = %+v", regs[0])
	}
	if regs[1].Step != "step-b" || regs[1].Position != "after:promotions" {
		t.Fatalf("second = %+v", regs[1])
	}
}

func TestImport_RegisterRow(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "importdemo/reference")
	if err := sdk.Import().RegisterProductRow(100, func(*extapi.ImportRowContext) error {
		return nil
	}); err != nil {
		t.Fatalf("RegisterProductRow: %v", err)
	}
	if app.ImportRegistry() == nil {
		t.Fatal("expected import registry")
	}
}

func TestSyncJobs_RegisterCron(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	sdk := pluginsdk.New(app, "warehousedemo/reference")

	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("warehousedemo").RegisterCron("warehouse.stock", "@every 5m", handler); err != nil {
		t.Fatalf("RegisterCron: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.warehousedemo.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
}

func TestSyncJobs_RegisterOnEvent(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	sdk := pluginsdk.New(app, "acme/reference")

	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("acme").RegisterOnEvent("pim.enrich", "catalog.product.updated", handler, pluginsdk.MaxRetries(3)); err != nil {
		t.Fatalf("RegisterOnEvent: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].Job.MaxRetries != 3 {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
}

func TestNew_PanicsOnNilApp(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil app")
		}
	}()
	pluginsdk.New(nil, "x")
}
