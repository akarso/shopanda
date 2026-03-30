package composition_test

import (
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// --- test helpers ---

type addBlockStep struct {
	blockType string
}

func (s addBlockStep) Name() string { return "add_block_" + s.blockType }

func (s addBlockStep) Apply(ctx *composition.ProductContext) error {
	ctx.Blocks = append(ctx.Blocks, composition.Block{
		Type: s.blockType,
		Data: map[string]interface{}{"source": s.Name()},
	})
	return nil
}

type failingStep struct{}

func (failingStep) Name() string                                  { return "failing" }
func (failingStep) Apply(_ *composition.ProductContext) error { return errors.New("boom") }

type listingBlockStep struct {
	blockType string
}

func (s listingBlockStep) Name() string { return "listing_" + s.blockType }

func (s listingBlockStep) Apply(ctx *composition.ListingContext) error {
	ctx.Blocks = append(ctx.Blocks, composition.Block{
		Type: s.blockType,
		Data: map[string]interface{}{"from": s.Name()},
	})
	return nil
}

type addFilterStep struct{}

func (addFilterStep) Name() string { return "add_filter" }

func (addFilterStep) Apply(ctx *composition.ListingContext) error {
	ctx.Filters = append(ctx.Filters, composition.Filter{
		Name:   "color",
		Values: []string{"red", "blue"},
	})
	return nil
}

// --- Pipeline[ProductContext] tests ---

func TestPipeline_Execute_Empty(t *testing.T) {
	p := composition.NewPipeline[composition.ProductContext]()
	prod := catalog.Product{ID: "1", Name: "Test"}
	ctx := composition.NewProductContext(&prod)

	if err := p.Execute(ctx); err != nil {
		t.Fatalf("Execute empty pipeline: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Errorf("blocks = %d, want 0", len(ctx.Blocks))
	}
}

func TestPipeline_Execute_SingleStep(t *testing.T) {
	p := composition.NewPipeline[composition.ProductContext]()
	p.AddStep(addBlockStep{blockType: "price"})

	prod := catalog.Product{ID: "1", Name: "Test"}
	ctx := composition.NewProductContext(&prod)

	if err := p.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
	}
	if ctx.Blocks[0].Type != "price" {
		t.Errorf("block type = %q, want price", ctx.Blocks[0].Type)
	}
}

func TestPipeline_Execute_MultipleSteps_Ordered(t *testing.T) {
	p := composition.NewPipeline[composition.ProductContext]()
	p.AddStep(addBlockStep{blockType: "base"})
	p.AddStep(addBlockStep{blockType: "pricing"})
	p.AddStep(addBlockStep{blockType: "availability"})

	prod := catalog.Product{ID: "1", Name: "Test"}
	ctx := composition.NewProductContext(&prod)

	if err := p.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(ctx.Blocks))
	}
	want := []string{"base", "pricing", "availability"}
	for i, w := range want {
		if ctx.Blocks[i].Type != w {
			t.Errorf("blocks[%d].Type = %q, want %q", i, ctx.Blocks[i].Type, w)
		}
	}
}

func TestPipeline_Execute_StopOnError(t *testing.T) {
	p := composition.NewPipeline[composition.ProductContext]()
	p.AddStep(addBlockStep{blockType: "base"})
	p.AddStep(failingStep{})
	p.AddStep(addBlockStep{blockType: "never"})

	prod := catalog.Product{ID: "1", Name: "Test"}
	ctx := composition.NewProductContext(&prod)

	err := p.Execute(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(ctx.Blocks) != 1 {
		t.Errorf("blocks = %d, want 1 (only base before failure)", len(ctx.Blocks))
	}
}

func TestPipeline_Steps_ReturnsNames(t *testing.T) {
	p := composition.NewPipeline[composition.ProductContext]()
	p.AddStep(addBlockStep{blockType: "base"})
	p.AddStep(addBlockStep{blockType: "pricing"})

	names := p.Steps()
	if len(names) != 2 {
		t.Fatalf("len = %d, want 2", len(names))
	}
	if names[0] != "add_block_base" {
		t.Errorf("names[0] = %q, want add_block_base", names[0])
	}
	if names[1] != "add_block_pricing" {
		t.Errorf("names[1] = %q, want add_block_pricing", names[1])
	}
}

// --- Pipeline[ListingContext] tests ---

func TestListingPipeline_Execute(t *testing.T) {
	p := composition.NewPipeline[composition.ListingContext]()
	p.AddStep(listingBlockStep{blockType: "grid"})
	p.AddStep(addFilterStep{})

	products := []catalog.Product{
		{ID: "1", Name: "A"},
		{ID: "2", Name: "B"},
	}
	ctx := composition.NewListingContext(products)

	if err := p.Execute(ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Blocks) != 1 {
		t.Errorf("blocks = %d, want 1", len(ctx.Blocks))
	}
	if len(ctx.Filters) != 1 {
		t.Errorf("filters = %d, want 1", len(ctx.Filters))
	}
	if ctx.Filters[0].Name != "color" {
		t.Errorf("filter name = %q, want color", ctx.Filters[0].Name)
	}
}

// --- Context constructor tests ---

func TestNewProductContext(t *testing.T) {
	prod := catalog.Product{ID: "1", Name: "Widget"}
	ctx := composition.NewProductContext(&prod)

	if ctx.Product != &prod {
		t.Error("Product pointer mismatch")
	}
	if ctx.Blocks == nil {
		t.Error("Blocks should be initialised")
	}
	if ctx.Meta == nil {
		t.Error("Meta should be initialised")
	}
}

func TestNewListingContext(t *testing.T) {
	products := []catalog.Product{{ID: "1"}}
	ctx := composition.NewListingContext(products)

	if len(ctx.Products) != 1 {
		t.Errorf("Products len = %d, want 1", len(ctx.Products))
	}
	if ctx.Filters == nil {
		t.Error("Filters should be initialised")
	}
	if ctx.SortOptions == nil {
		t.Error("SortOptions should be initialised")
	}
	if ctx.Blocks == nil {
		t.Error("Blocks should be initialised")
	}
	if ctx.Meta == nil {
		t.Error("Meta should be initialised")
	}
}
