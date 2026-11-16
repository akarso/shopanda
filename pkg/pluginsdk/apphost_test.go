package pluginsdk_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// TestAppHost_RegisterPricingStep_InvalidTypePanics covers PR-1546 review
// fix: Host.RegisterPricingStep must fail closed (panic) on an uncoercible
// step instead of silently registering it — a silently-dropped step would
// otherwise only surface much later as "plugin.step.invalid_type" at wiring
// time, never during the plugin's own Init.
func TestAppHost_RegisterPricingStep_InvalidTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for uncoercible pricing step")
		}
	}()
	h := pluginsdk.AsHost(testApp())
	h.RegisterPricingStep(42)
}

func TestAppHost_RegisterPricingStep_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil pricing step")
		}
	}()
	h := pluginsdk.AsHost(testApp())
	h.RegisterPricingStep(nil)
}

func TestAppHost_RegisterCheckoutStep_InvalidTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for uncoercible checkout step")
		}
	}()
	h := pluginsdk.AsHost(testApp())
	h.RegisterCheckoutStep("not-a-step")
}

func TestAppHost_RegisterCompositionStep_InvalidTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for uncoercible pdp composition step")
		}
	}()
	h := pluginsdk.AsHost(testApp())
	h.RegisterCompositionStep("pdp", "not-a-step")
}

func TestAppHost_RegisterCompositionStep_ValidPasses(t *testing.T) {
	h := pluginsdk.AsHost(testApp())
	h.RegisterCompositionStep("pdp", fakeCompositionStep{name: "ok"})
}

// Unknown pipeline names cannot be type-checked against a known public or
// domain contract, so they fail closed (panic) just like a bad pdp/plp step
// type — a typo'd pipeline name would otherwise only surface much later as a
// dropped step at wiring time, never during the plugin's own Init.
func TestAppHost_RegisterCompositionStep_UnknownPipelinePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for unknown composition pipeline")
		}
	}()
	h := pluginsdk.AsHost(testApp())
	h.RegisterCompositionStep("unknown-pipeline", "anything")
}
