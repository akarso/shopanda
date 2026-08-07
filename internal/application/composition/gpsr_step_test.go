package composition_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestGpsrStep_AddsBlockWhenEnabled(t *testing.T) {
	cfg := stubOmnibusConfig{
		legal.ScopedConfigKey("store-eu", legal.GpsrEnabledConfigKey):             true,
		legal.ScopedConfigKey("store-eu", legal.GpsrManufacturerNameConfigKey):    "EU Safety Co.",
		legal.ScopedConfigKey("store-eu", legal.GpsrManufacturerContactConfigKey): "safety@eu.example",
	}
	step := composition.NewGpsrStep(cfg)
	prod := catalog.Product{
		ID:   "p1",
		Name: "Toy",
		Attributes: map[string]interface{}{
			legal.AttrGpsrSafetyWarnings: "Small parts.",
			legal.AttrGpsrAgeRestriction: "3_plus",
		},
	}
	ctx := composition.NewProductContext(&prod)
	ctx.Ctx = context.Background()
	ctx.StoreID = "store-eu"

	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 || ctx.Blocks[0].Type != "gpsr_safety_disclosure" {
		t.Fatalf("blocks = %+v", ctx.Blocks)
	}
	if ctx.Blocks[0].Data["manufacturer_name"] != "EU Safety Co." {
		t.Fatalf("manufacturer = %v", ctx.Blocks[0].Data["manufacturer_name"])
	}
}

func TestGpsrStep_SkipsWhenDisabled(t *testing.T) {
	cfg := stubOmnibusConfig{
		legal.ScopedConfigKey("store-eu", legal.GpsrEnabledConfigKey): false,
	}
	step := composition.NewGpsrStep(cfg)
	prod := catalog.Product{
		ID: "p1",
		Attributes: map[string]interface{}{
			legal.AttrGpsrSafetyWarnings: "Warning",
		},
	}
	ctx := composition.NewProductContext(&prod)
	ctx.Ctx = context.Background()
	ctx.StoreID = "store-eu"

	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Fatalf("expected no blocks, got %+v", ctx.Blocks)
	}
}
