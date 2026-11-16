package pluginsdk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// named is satisfied by the pricing/checkout/composition adapter bridges
// (pricingStepBridge, checkoutStepBridge, productCompositionBridge) so tests
// can assert on a registered step's identity after RegisterPricingStep /
// RegisterCheckoutStep / RegisterCompositionStep coerce it (PR-1546 review:
// Register* now panics on an uncoercible step instead of registering it
// as-is, so tests must register real extapi.*Step fakes, not bare strings).
type named interface {
	Name() string
}

type fakePricingStep struct{ name string }

func (f fakePricingStep) Name() string                                       { return f.name }
func (f fakePricingStep) Apply(context.Context, extapi.PricingContext) error { return nil }

type fakeCheckoutStep struct{ name string }

func (f fakeCheckoutStep) Name() string                                          { return f.name }
func (f fakeCheckoutStep) Execute(context.Context, extapi.CheckoutContext) error { return nil }

type fakeCompositionStep struct{ name string }

func (f fakeCompositionStep) Name() string                                 { return f.name }
func (f fakeCompositionStep) Apply(extapi.ProductCompositionContext) error { return nil }

type hostOnlyPlugin struct {
	inited bool
	name   string
}

func (p *hostOnlyPlugin) Name() string { return p.name }

func (p *hostOnlyPlugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return errNilHost
	}
	sdk := pluginsdk.NewHost(h, p.name)
	sdk.Pricing().Register(fakePricingStep{name: "host-step"}, pluginsdk.After("base"))
	sdk.Checkout().Register(fakeCheckoutStep{name: "host-checkout"}, pluginsdk.CheckoutEnd())
	h.RegisterCompositionStep("pdp", fakeCompositionStep{name: "host-comp"})
	_ = h.Logger()
	_ = h.Context()
	p.inited = true
	return nil
}

var errNilHost = errors.New("pluginsdk_test: nil host")

func TestAdapt_InitUsesHost(t *testing.T) {
	app := testApp()
	p := &hostOnlyPlugin{name: "acme/hostonly"}
	bridged := pluginsdk.Adapt(p)
	if bridged.Name() != "acme/hostonly" {
		t.Fatalf("Name = %q", bridged.Name())
	}
	if err := bridged.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !p.inited {
		t.Fatal("expected Init via Host")
	}
	regs := app.PricingStepRegistrations()
	if len(regs) != 1 || regs[0].Step.(named).Name() != "host-step" {
		t.Fatalf("pricing regs = %+v", regs)
	}
	cregs := app.CheckoutStepRegistrations()
	if len(cregs) != 1 || cregs[0].Step.(named).Name() != "host-checkout" {
		t.Fatalf("checkout regs = %+v", cregs)
	}
}

func TestAsHost_RegisterPricing(t *testing.T) {
	app := testApp()
	h := pluginsdk.AsHost(app)
	h.RegisterPricingStep(fakePricingStep{name: "s1"}, "after:base")
	regs := app.PricingStepRegistrations()
	if len(regs) != 1 || regs[0].Position != "after:base" {
		t.Fatalf("regs = %+v", regs)
	}
}

func TestAsHost_RegisterPricing_InvalidStepPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for uncoercible pricing step")
		}
	}()
	app := testApp()
	h := pluginsdk.AsHost(app)
	h.RegisterPricingStep("not-a-step", "after:base")
}

func TestNewHost_PricingRegister(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.NewHost(pluginsdk.AsHost(app), "acme/demo")
	sdk.Pricing().Register(fakePricingStep{name: "x"}, pluginsdk.Before("tax"))
	regs := app.PricingStepRegistrations()
	if len(regs) != 1 || regs[0].Position != "before:tax" {
		t.Fatalf("regs = %+v", regs)
	}
}

var _ plugin.Plugin = pluginsdk.Adapt(&hostOnlyPlugin{name: "x"})
