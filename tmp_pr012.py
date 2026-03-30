#!/usr/bin/env python3
"""Create PR-012 files: composition pipeline, step interface, contexts, tests."""
import os

BASE = os.path.dirname(os.path.abspath(__file__))

files = {}

# --- Block type ---
files["internal/application/composition/block.go"] = """\
package composition

// Block represents a UI-agnostic component attached to a composition context.
type Block struct {
\tType string
\tData map[string]interface{}
}
"""

# --- Step interface (generic) ---
files["internal/application/composition/step.go"] = """\
package composition

// Step defines a single transformation in a composition pipeline.
// Each step operates on a typed context pointer.
type Step[T any] interface {
\tName() string
\tApply(ctx *T) error
}
"""

# --- Pipeline (generic) ---
files["internal/application/composition/pipeline.go"] = """\
package composition

import "fmt"

// Pipeline executes a sequence of steps against a typed context.
type Pipeline[T any] struct {
\tsteps []Step[T]
}

// NewPipeline creates an empty pipeline.
func NewPipeline[T any]() *Pipeline[T] {
\treturn &Pipeline[T]{}
}

// AddStep appends a step to the end of the pipeline.
func (p *Pipeline[T]) AddStep(s Step[T]) {
\tp.steps = append(p.steps, s)
}

// Execute runs all steps in order against ctx.
// If any step returns an error the pipeline stops and returns it.
func (p *Pipeline[T]) Execute(ctx *T) error {
\tfor _, s := range p.steps {
\t\tif err := s.Apply(ctx); err != nil {
\t\t\treturn fmt.Errorf(\"pipeline step %q: %w\", s.Name(), err)
\t\t}
\t}
\treturn nil
}

// Steps returns the names of all registered steps in order.
func (p *Pipeline[T]) Steps() []string {
\tnames := make([]string, len(p.steps))
\tfor i, s := range p.steps {
\t\tnames[i] = s.Name()
\t}
\treturn names
}
"""

# --- Product context (PDP) ---
files["internal/application/composition/product_context.go"] = """\
package composition

import "github.com/akarso/shopanda/internal/domain/catalog"

// ProductContext holds the data built up during product page composition (PDP).
type ProductContext struct {
\tProduct  *catalog.Product
\tCurrency string
\tCountry  string
\tBlocks   []Block
\tMeta     map[string]interface{}
}

// NewProductContext creates a ProductContext for the given product.
func NewProductContext(p *catalog.Product) *ProductContext {
\treturn &ProductContext{
\t\tProduct: p,
\t\tBlocks:  make([]Block, 0),
\t\tMeta:    make(map[string]interface{}),
\t}
}
"""

# --- Listing context (PLP) ---
files["internal/application/composition/listing_context.go"] = """\
package composition

import "github.com/akarso/shopanda/internal/domain/catalog"

// Filter represents a facet option available in a product listing.
type Filter struct {
\tName   string
\tValues []string
}

// SortOption represents a sorting choice available in a product listing.
type SortOption struct {
\tName  string
\tField string
\tDir   string // \"asc\" or \"desc\"
}

// ListingContext holds the data built up during product listing composition (PLP).
type ListingContext struct {
\tProducts    []catalog.Product
\tFilters     []Filter
\tSortOptions []SortOption
\tBlocks      []Block
\tCurrency    string
\tCountry     string
\tMeta        map[string]interface{}
}

// NewListingContext creates a ListingContext for the given products.
func NewListingContext(products []catalog.Product) *ListingContext {
\treturn &ListingContext{
\t\tProducts:    products,
\t\tFilters:     make([]Filter, 0),
\t\tSortOptions: make([]SortOption, 0),
\t\tBlocks:      make([]Block, 0),
\t\tMeta:        make(map[string]interface{}),
\t}
}
"""

# --- Tests ---
files["internal/application/composition/pipeline_test.go"] = """\
package composition_test

import (
\t"errors"
\t"testing"

\t"github.com/akarso/shopanda/internal/application/composition"
\t"github.com/akarso/shopanda/internal/domain/catalog"
)

// --- test helpers ---

type addBlockStep struct {
\tblockType string
}

func (s addBlockStep) Name() string { return "add_block_" + s.blockType }

func (s addBlockStep) Apply(ctx *composition.ProductContext) error {
\tctx.Blocks = append(ctx.Blocks, composition.Block{
\t\tType: s.blockType,
\t\tData: map[string]interface{}{"source": s.Name()},
\t})
\treturn nil
}

type failingStep struct{}

func (failingStep) Name() string                                  { return "failing" }
func (failingStep) Apply(_ *composition.ProductContext) error { return errors.New("boom") }

type listingBlockStep struct {
\tblockType string
}

func (s listingBlockStep) Name() string { return "listing_" + s.blockType }

func (s listingBlockStep) Apply(ctx *composition.ListingContext) error {
\tctx.Blocks = append(ctx.Blocks, composition.Block{
\t\tType: s.blockType,
\t\tData: map[string]interface{}{"from": s.Name()},
\t})
\treturn nil
}

type addFilterStep struct{}

func (addFilterStep) Name() string { return "add_filter" }

func (addFilterStep) Apply(ctx *composition.ListingContext) error {
\tctx.Filters = append(ctx.Filters, composition.Filter{
\t\tName:   "color",
\t\tValues: []string{"red", "blue"},
\t})
\treturn nil
}

// --- Pipeline[ProductContext] tests ---

func TestPipeline_Execute_Empty(t *testing.T) {
\tp := composition.NewPipeline[composition.ProductContext]()
\tprod := catalog.Product{ID: "1", Name: "Test"}
\tctx := composition.NewProductContext(&prod)

\tif err := p.Execute(ctx); err != nil {
\t\tt.Fatalf("Execute empty pipeline: %v", err)
\t}
\tif len(ctx.Blocks) != 0 {
\t\tt.Errorf("blocks = %d, want 0", len(ctx.Blocks))
\t}
}

func TestPipeline_Execute_SingleStep(t *testing.T) {
\tp := composition.NewPipeline[composition.ProductContext]()
\tp.AddStep(addBlockStep{blockType: "price"})

\tprod := catalog.Product{ID: "1", Name: "Test"}
\tctx := composition.NewProductContext(&prod)

\tif err := p.Execute(ctx); err != nil {
\t\tt.Fatalf("Execute: %v", err)
\t}
\tif len(ctx.Blocks) != 1 {
\t\tt.Fatalf("blocks = %d, want 1", len(ctx.Blocks))
\t}
\tif ctx.Blocks[0].Type != "price" {
\t\tt.Errorf("block type = %q, want price", ctx.Blocks[0].Type)
\t}
}

func TestPipeline_Execute_MultipleSteps_Ordered(t *testing.T) {
\tp := composition.NewPipeline[composition.ProductContext]()
\tp.AddStep(addBlockStep{blockType: "base"})
\tp.AddStep(addBlockStep{blockType: "pricing"})
\tp.AddStep(addBlockStep{blockType: "availability"})

\tprod := catalog.Product{ID: "1", Name: "Test"}
\tctx := composition.NewProductContext(&prod)

\tif err := p.Execute(ctx); err != nil {
\t\tt.Fatalf("Execute: %v", err)
\t}
\tif len(ctx.Blocks) != 3 {
\t\tt.Fatalf("blocks = %d, want 3", len(ctx.Blocks))
\t}
\twant := []string{"base", "pricing", "availability"}
\tfor i, w := range want {
\t\tif ctx.Blocks[i].Type != w {
\t\t\tt.Errorf("blocks[%d].Type = %q, want %q", i, ctx.Blocks[i].Type, w)
\t\t}
\t}
}

func TestPipeline_Execute_StopOnError(t *testing.T) {
\tp := composition.NewPipeline[composition.ProductContext]()
\tp.AddStep(addBlockStep{blockType: "base"})
\tp.AddStep(failingStep{})
\tp.AddStep(addBlockStep{blockType: "never"})

\tprod := catalog.Product{ID: "1", Name: "Test"}
\tctx := composition.NewProductContext(&prod)

\terr := p.Execute(ctx)
\tif err == nil {
\t\tt.Fatal("expected error")
\t}
\tif len(ctx.Blocks) != 1 {
\t\tt.Errorf("blocks = %d, want 1 (only base before failure)", len(ctx.Blocks))
\t}
}

func TestPipeline_Steps_ReturnsNames(t *testing.T) {
\tp := composition.NewPipeline[composition.ProductContext]()
\tp.AddStep(addBlockStep{blockType: "base"})
\tp.AddStep(addBlockStep{blockType: "pricing"})

\tnames := p.Steps()
\tif len(names) != 2 {
\t\tt.Fatalf("len = %d, want 2", len(names))
\t}
\tif names[0] != "add_block_base" {
\t\tt.Errorf("names[0] = %q, want add_block_base", names[0])
\t}
\tif names[1] != "add_block_pricing" {
\t\tt.Errorf("names[1] = %q, want add_block_pricing", names[1])
\t}
}

// --- Pipeline[ListingContext] tests ---

func TestListingPipeline_Execute(t *testing.T) {
\tp := composition.NewPipeline[composition.ListingContext]()
\tp.AddStep(listingBlockStep{blockType: "grid"})
\tp.AddStep(addFilterStep{})

\tproducts := []catalog.Product{
\t\t{ID: "1", Name: "A"},
\t\t{ID: "2", Name: "B"},
\t}
\tctx := composition.NewListingContext(products)

\tif err := p.Execute(ctx); err != nil {
\t\tt.Fatalf("Execute: %v", err)
\t}
\tif len(ctx.Blocks) != 1 {
\t\tt.Errorf("blocks = %d, want 1", len(ctx.Blocks))
\t}
\tif len(ctx.Filters) != 1 {
\t\tt.Errorf("filters = %d, want 1", len(ctx.Filters))
\t}
\tif ctx.Filters[0].Name != "color" {
\t\tt.Errorf("filter name = %q, want color", ctx.Filters[0].Name)
\t}
}

// --- Context constructor tests ---

func TestNewProductContext(t *testing.T) {
\tprod := catalog.Product{ID: "1", Name: "Widget"}
\tctx := composition.NewProductContext(&prod)

\tif ctx.Product != &prod {
\t\tt.Error("Product pointer mismatch")
\t}
\tif ctx.Blocks == nil {
\t\tt.Error("Blocks should be initialised")
\t}
\tif ctx.Meta == nil {
\t\tt.Error("Meta should be initialised")
\t}
}

func TestNewListingContext(t *testing.T) {
\tproducts := []catalog.Product{{ID: "1"}}
\tctx := composition.NewListingContext(products)

\tif len(ctx.Products) != 1 {
\t\tt.Errorf("Products len = %d, want 1", len(ctx.Products))
\t}
\tif ctx.Filters == nil {
\t\tt.Error("Filters should be initialised")
\t}
\tif ctx.SortOptions == nil {
\t\tt.Error("SortOptions should be initialised")
\t}
\tif ctx.Blocks == nil {
\t\tt.Error("Blocks should be initialised")
\t}
\tif ctx.Meta == nil {
\t\tt.Error("Meta should be initialised")
\t}
}
"""

for rel_path, content in files.items():
    full_path = os.path.join(BASE, rel_path)
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    with open(full_path, "w") as f:
        f.write(content)
    print(f"created {rel_path}")
