package checkout_test

import (
	"context"
	"testing"

	checkoutapp "github.com/akarso/shopanda/internal/application/checkout"
)

type stubStep struct {
	name string
}

func (s stubStep) Name() string { return s.name }

func (s stubStep) Execute(_ context.Context, _ *checkoutapp.Context) error { return nil }

func coreWorkflow() []checkoutapp.Step {
	return []checkoutapp.Step{
		stubStep{name: "validate_cart"},
		stubStep{name: "recalculate_pricing"},
		stubStep{name: "reserve_inventory"},
		stubStep{name: "create_order"},
		stubStep{name: "select_shipping"},
		stubStep{name: "initiate_payment"},
	}
}

func names(steps []checkoutapp.Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name()
	}
	return out
}

func TestParseStepPosition_DefaultAndShortcuts(t *testing.T) {
	mode, anchor, err := checkoutapp.ParseStepPosition("")
	if err != nil || mode != "after" || anchor != "initiate_payment" {
		t.Fatalf("default = %q %q err=%v", mode, anchor, err)
	}

	mode, anchor, err = checkoutapp.ParseStepPosition("start")
	if err != nil || mode != "before" || anchor != "validate_cart" {
		t.Fatalf("start = %q %q err=%v", mode, anchor, err)
	}

	_, anchor, err = checkoutapp.ParseStepPosition("before:order")
	if err != nil || anchor != "create_order" {
		t.Fatalf("order alias = %q err=%v", anchor, err)
	}
}

func TestParseStepPosition_Invalid(t *testing.T) {
	if _, _, err := checkoutapp.ParseStepPosition("create_order"); err == nil {
		t.Fatal("expected error for bare anchor")
	}
	if _, _, err := checkoutapp.ParseStepPosition("after:unknown"); err == nil {
		t.Fatal("expected error for unknown anchor")
	}
}

func TestMergePluginSteps_DefaultEnd(t *testing.T) {
	merged, err := checkoutapp.MergePluginSteps(coreWorkflow(), []checkoutapp.PluginStepRegistration{
		{Step: stubStep{name: "plugin.audit"}, Position: checkoutapp.DefaultPluginStepPosition},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"validate_cart", "recalculate_pricing", "reserve_inventory", "create_order", "select_shipping", "initiate_payment", "plugin.audit"}
	if got := names(merged); len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestMergePluginSteps_Start(t *testing.T) {
	merged, err := checkoutapp.MergePluginSteps(coreWorkflow(), []checkoutapp.PluginStepRegistration{
		{Step: stubStep{name: "plugin.preflight"}, Position: "start"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"plugin.preflight", "validate_cart", "recalculate_pricing", "reserve_inventory", "create_order", "select_shipping", "initiate_payment"}
	if got := names(merged); len(got) != len(want) || got[0] != "plugin.preflight" {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestMergePluginSteps_BeforeCreateOrder(t *testing.T) {
	merged, err := checkoutapp.MergePluginSteps(coreWorkflow(), []checkoutapp.PluginStepRegistration{
		{Step: stubStep{name: "plugin.fraud"}, Position: "before:create_order"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"validate_cart", "recalculate_pricing", "reserve_inventory", "plugin.fraud", "create_order", "select_shipping", "initiate_payment"}
	if got := names(merged); len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestMergePluginSteps_SameAnchorPreservesRegistrationOrder(t *testing.T) {
	merged, err := checkoutapp.MergePluginSteps(coreWorkflow(), []checkoutapp.PluginStepRegistration{
		{Step: stubStep{name: "first"}, Position: "after:validate_cart"},
		{Step: stubStep{name: "second"}, Position: "after:validate_cart"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	if got := names(merged)[1:3]; got[0] != "first" || got[1] != "second" {
		t.Fatalf("order = %v", got)
	}
}

func TestMergePluginSteps_MissingCoreAnchorRejected(t *testing.T) {
	coreWithoutShipping := []checkoutapp.Step{
		stubStep{name: "validate_cart"},
		stubStep{name: "recalculate_pricing"},
		stubStep{name: "reserve_inventory"},
		stubStep{name: "create_order"},
		stubStep{name: "initiate_payment"},
	}
	_, err := checkoutapp.MergePluginSteps(coreWithoutShipping, []checkoutapp.PluginStepRegistration{
		{Step: stubStep{name: "orphan"}, Position: "after:select_shipping"},
	})
	if err == nil {
		t.Fatal("expected error when anchor missing from core workflow")
	}
}
