package extapi_test

import (
	"testing"

	checkoutapp "github.com/akarso/shopanda/internal/application/checkout"
	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	apppricing "github.com/akarso/shopanda/internal/application/pricing"
	slotsapp "github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestStableHookPointsMatchInternal(t *testing.T) {
	internal := hooksapp.CartHookPoints()
	stable := extapi.HookPoints()
	if len(stable) != len(internal) {
		t.Fatalf("stable hook points len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("hook[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestStableSlotAnchorsMatchInternalCatalog(t *testing.T) {
	internal := slotsapp.StandardAnchorNames()
	stable := extapi.SlotAnchorNames()
	if len(stable) != len(internal) {
		t.Fatalf("stable anchors len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("anchor[%d] = %q, want %q", i, stable[i], name)
		}
	}

	for _, anchor := range extapi.SlotAnchors() {
		if string(anchor) == "" {
			t.Fatal("slot anchor constant must not be empty")
		}
	}
}

func TestStablePricingStepCatalog(t *testing.T) {
	stable := extapi.PricingStepCatalog()
	internal := apppricing.CoreStepCatalog
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("step[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestStableCheckoutStepCatalog(t *testing.T) {
	stable := extapi.CheckoutStepCatalog()
	internal := checkoutapp.CoreStepCatalog
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("step[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestReplacePricingStep(t *testing.T) {
	if got := extapi.ReplacePricingStep("tax"); got != "replace:tax" {
		t.Fatalf("ReplacePricingStep() = %q", got)
	}
	mode, anchor, err := apppricing.ParseStepPosition(extapi.ReplacePricingStep("taxes"))
	if err != nil || mode != apppricing.StepPositionReplace || anchor != "tax" {
		t.Fatalf("parse replace = %q %q err=%v", mode, anchor, err)
	}
}

func TestStablePlacementsMatchInternal(t *testing.T) {
	cases := []struct {
		stable extapi.Placement
		want   slotsapp.Placement
	}{
		{extapi.PlacementBefore, slotsapp.PlacementBefore},
		{extapi.PlacementAfter, slotsapp.PlacementAfter},
		{extapi.PlacementPrepend, slotsapp.PlacementPrepend},
		{extapi.PlacementAppend, slotsapp.PlacementAppend},
	}
	for _, tc := range cases {
		if string(tc.stable) != string(tc.want) {
			t.Fatalf("placement %q != %q", tc.stable, tc.want)
		}
	}
}
