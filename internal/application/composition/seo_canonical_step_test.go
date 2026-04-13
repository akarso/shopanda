package composition_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestCanonicalURLStep_Name(t *testing.T) {
	s := composition.NewCanonicalURLStep("https://example.com")
	if s.Name() != "seo_canonical" {
		t.Errorf("Name() = %q, want seo_canonical", s.Name())
	}
}

func TestCanonicalURLStep_Apply(t *testing.T) {
	prod := catalog.Product{ID: "1", Name: "Widget", Slug: "widget"}
	ctx := composition.NewProductContext(&prod)
	s := composition.NewCanonicalURLStep("https://example.com")

	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Meta["canonical"] != "https://example.com/products/widget" {
		t.Errorf("canonical = %v, want https://example.com/products/widget", ctx.Meta["canonical"])
	}
}

func TestCanonicalURLStep_NilProduct(t *testing.T) {
	ctx := composition.NewProductContext(nil)
	s := composition.NewCanonicalURLStep("https://example.com")

	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := ctx.Meta["canonical"]; ok {
		t.Error("expected no canonical for nil product")
	}
}

func TestCanonicalURLStep_NilCtx(t *testing.T) {
	s := composition.NewCanonicalURLStep("https://example.com")
	if err := s.Apply(nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}
}

func TestCanonicalURLStep_EmptySlug(t *testing.T) {
	prod := catalog.Product{ID: "1", Name: "Widget", Slug: ""}
	ctx := composition.NewProductContext(&prod)
	s := composition.NewCanonicalURLStep("https://example.com")

	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := ctx.Meta["canonical"]; ok {
		t.Error("expected no canonical for empty slug")
	}
}

func TestListingCanonicalURLStep_Name(t *testing.T) {
	s := composition.NewListingCanonicalURLStep("https://example.com")
	if s.Name() != "seo_canonical" {
		t.Errorf("Name() = %q, want seo_canonical", s.Name())
	}
}

func TestListingCanonicalURLStep_Apply(t *testing.T) {
	ctx := composition.NewListingContext(nil)
	s := composition.NewListingCanonicalURLStep("https://example.com")

	if err := s.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Meta["canonical"] != "https://example.com/products" {
		t.Errorf("canonical = %v, want https://example.com/products", ctx.Meta["canonical"])
	}
}

func TestListingCanonicalURLStep_NilCtx(t *testing.T) {
	s := composition.NewListingCanonicalURLStep("https://example.com")
	if err := s.Apply(nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}
}
