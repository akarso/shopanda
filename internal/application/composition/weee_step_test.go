package composition_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestWeeeStep_AddsBlockWhenEnabled(t *testing.T) {
	cfg := stubOmnibusConfig{
		legal.ScopedConfigKey("store-eu", legal.WeeeEnabledConfigKey):               true,
		legal.ScopedConfigKey("store-eu", legal.WeeeProducerRegistrationConfigKey): "PL-WEEE-1",
	}
	step := composition.NewWeeeStep(cfg)
	prod := catalog.Product{
		ID:   "p1",
		Name: "Mouse",
		Attributes: map[string]interface{}{
			legal.AttrWeeeCategory:      "small_it_telecom",
			legal.AttrWeeeSymbolVisible: true,
		},
	}
	ctx := composition.NewProductContext(&prod)
	ctx.Ctx = context.Background()
	ctx.StoreID = "store-eu"

	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 1 || ctx.Blocks[0].Type != "weee_disclosure" {
		t.Fatalf("blocks = %+v", ctx.Blocks)
	}
	if ctx.Blocks[0].Data["producer_registration"] != "PL-WEEE-1" {
		t.Fatalf("registration = %v", ctx.Blocks[0].Data["producer_registration"])
	}
}

func TestWeeeStep_SkipsWhenDisabled(t *testing.T) {
	cfg := stubOmnibusConfig{
		legal.ScopedConfigKey("store-eu", legal.WeeeEnabledConfigKey): false,
	}
	step := composition.NewWeeeStep(cfg)
	prod := catalog.Product{
		ID: "p1",
		Attributes: map[string]interface{}{
			legal.AttrWeeeCategory: "lighting",
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

func TestWeeeStep_SkipsWhenNoData(t *testing.T) {
	cfg := stubOmnibusConfig{
		legal.ScopedConfigKey("store-eu", legal.WeeeEnabledConfigKey): true,
	}
	step := composition.NewWeeeStep(cfg)
	prod := catalog.Product{ID: "p1"}
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
