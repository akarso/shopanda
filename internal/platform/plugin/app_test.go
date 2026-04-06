package plugin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_RegisterPricingStep(t *testing.T) {
	app := &plugin.App{}
	app.RegisterPricingStep("step1")
	app.RegisterPricingStep("step2")

	steps := app.PricingSteps()
	if len(steps) != 2 {
		t.Fatalf("PricingSteps() len = %d, want 2", len(steps))
	}
	if steps[0] != "step1" || steps[1] != "step2" {
		t.Errorf("PricingSteps() = %v, want [step1 step2]", steps)
	}
}

func TestApp_RegisterPricingStep_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil pricing step")
		}
	}()
	app := &plugin.App{}
	app.RegisterPricingStep(nil)
}

func TestApp_RegisterCheckoutStep(t *testing.T) {
	app := &plugin.App{}
	app.RegisterCheckoutStep("step1")

	steps := app.CheckoutSteps()
	if len(steps) != 1 {
		t.Fatalf("CheckoutSteps() len = %d, want 1", len(steps))
	}
}

func TestApp_RegisterCheckoutStep_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil checkout step")
		}
	}()
	app := &plugin.App{}
	app.RegisterCheckoutStep(nil)
}

func TestApp_RegisterCompositionStep(t *testing.T) {
	app := &plugin.App{}
	app.RegisterCompositionStep("pdp", "step1")
	app.RegisterCompositionStep("plp", "step2")
	app.RegisterCompositionStep("pdp", "step3")

	pdp := app.CompositionSteps("pdp")
	if len(pdp) != 2 {
		t.Fatalf("CompositionSteps(pdp) len = %d, want 2", len(pdp))
	}
	plp := app.CompositionSteps("plp")
	if len(plp) != 1 {
		t.Fatalf("CompositionSteps(plp) len = %d, want 1", len(plp))
	}
}

func TestApp_RegisterCompositionStep_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil composition step")
		}
	}()
	app := &plugin.App{}
	app.RegisterCompositionStep("pdp", nil)
}

func TestApp_RegisterCompositionStep_EmptyPipelinePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty pipeline name")
		}
	}()
	app := &plugin.App{}
	app.RegisterCompositionStep("", "step")
}

func TestApp_CompositionSteps_UnknownPipeline(t *testing.T) {
	app := &plugin.App{}
	if steps := app.CompositionSteps("unknown"); steps != nil {
		t.Fatalf("CompositionSteps(unknown) = %v, want nil", steps)
	}
}

func TestApp_PricingSteps_Empty(t *testing.T) {
	app := &plugin.App{}
	if steps := app.PricingSteps(); steps != nil {
		t.Fatalf("PricingSteps() = %v, want nil", steps)
	}
}

func TestApp_CheckoutSteps_Empty(t *testing.T) {
	app := &plugin.App{}
	if steps := app.CheckoutSteps(); steps != nil {
		t.Fatalf("CheckoutSteps() = %v, want nil", steps)
	}
}
