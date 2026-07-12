package pricing_test

import (
	"context"
	"testing"

	apppricing "github.com/akarso/shopanda/internal/application/pricing"
	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
)

type stubStep struct {
	name string
}

func (s stubStep) Name() string { return s.name }
func (s stubStep) Apply(context.Context, *domainpricing.PricingContext) error {
	return nil
}

func corePipeline() []domainpricing.PricingStep {
	return []domainpricing.PricingStep{
		stubStep{name: "base"},
		stubStep{name: "catalog_promotions"},
		stubStep{name: "cart_promotions"},
		stubStep{name: "tax"},
		stubStep{name: "finalize"},
	}
}

func names(steps []domainpricing.PricingStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name()
	}
	return out
}

func TestParseStepPosition_DefaultAndAliases(t *testing.T) {
	rel, anchor, err := apppricing.ParseStepPosition("")
	if err != nil || rel != "after" || anchor != "base" {
		t.Fatalf("default = %q %q err=%v", rel, anchor, err)
	}

	_, anchor, err = apppricing.ParseStepPosition("after:promotions")
	if err != nil || anchor != "cart_promotions" {
		t.Fatalf("promotions alias = %q err=%v", anchor, err)
	}

	_, anchor, err = apppricing.ParseStepPosition("before:taxes")
	if err != nil || anchor != "tax" {
		t.Fatalf("taxes alias = %q err=%v", anchor, err)
	}
}

func TestParseStepPosition_Invalid(t *testing.T) {
	if _, _, err := apppricing.ParseStepPosition("end"); err == nil {
		t.Fatal("expected error for bare anchor")
	}
	if _, _, err := apppricing.ParseStepPosition("after:unknown"); err == nil {
		t.Fatal("expected error for unknown anchor")
	}
}

func TestMergePluginSteps_DefaultAfterBase(t *testing.T) {
	merged, err := apppricing.MergePluginSteps(corePipeline(), []apppricing.PluginStepRegistration{
		{Step: stubStep{name: "plugin.fee"}, Position: apppricing.DefaultPluginStepPosition},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"base", "plugin.fee", "catalog_promotions", "cart_promotions", "tax", "finalize"}
	got := names(merged)
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
}

func TestMergePluginSteps_AfterPromotions(t *testing.T) {
	merged, err := apppricing.MergePluginSteps(corePipeline(), []apppricing.PluginStepRegistration{
		{Step: stubStep{name: "acme.volume"}, Position: "after:promotions"},
		{Step: stubStep{name: "acme.fee"}, Position: "after:cart_promotions"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"base", "catalog_promotions", "cart_promotions", "acme.volume", "acme.fee", "tax", "finalize"}
	if got := names(merged); got[3] != "acme.volume" || got[4] != "acme.fee" {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestMergePluginSteps_SameAnchorPreservesRegistrationOrder(t *testing.T) {
	merged, err := apppricing.MergePluginSteps(corePipeline(), []apppricing.PluginStepRegistration{
		{Step: stubStep{name: "first"}, Position: "after:base"},
		{Step: stubStep{name: "second"}, Position: "after:base"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	if got := names(merged)[1:3]; got[0] != "first" || got[1] != "second" {
		t.Fatalf("order = %v", got)
	}
}

func TestMergePluginSteps_BeforeTax(t *testing.T) {
	merged, err := apppricing.MergePluginSteps(corePipeline(), []apppricing.PluginStepRegistration{
		{Step: stubStep{name: "pre_tax"}, Position: "before:tax"},
	})
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}
	want := []string{"base", "catalog_promotions", "cart_promotions", "pre_tax", "tax", "finalize"}
	if got := names(merged); len(got) != len(want) || got[3] != "pre_tax" {
		t.Fatalf("names = %v, want %v", got, want)
	}
}
