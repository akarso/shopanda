package composition_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestProductMetaStep_Name(t *testing.T) {
	s := composition.ProductMetaStep{}
	if s.Name() != "seo_meta" {
		t.Errorf("Name() = %q, want seo_meta", s.Name())
	}
}

func TestProductMetaStep_Apply(t *testing.T) {
	prod := catalog.Product{ID: "1", Name: "Widget", Description: "A fine widget"}
	ctx := composition.NewProductContext(&prod)

	if err := (composition.ProductMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Meta["title"] != "Widget" {
		t.Errorf("title = %v, want Widget", ctx.Meta["title"])
	}
	if ctx.Meta["description"] != "A fine widget" {
		t.Errorf("description = %v, want A fine widget", ctx.Meta["description"])
	}
}

func TestProductMetaStep_Apply_TruncatesDescription(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "x"
	}
	prod := catalog.Product{ID: "1", Name: "P", Description: long}
	ctx := composition.NewProductContext(&prod)

	if err := (composition.ProductMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	desc, ok := ctx.Meta["description"].(string)
	if !ok {
		t.Fatal("description not a string")
	}
	if len(desc) != 160 {
		t.Errorf("description length = %d, want 160", len(desc))
	}
}

func TestProductMetaStep_Apply_NilProduct(t *testing.T) {
	ctx := composition.NewProductContext(nil)
	if err := (composition.ProductMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := ctx.Meta["title"]; ok {
		t.Error("expected no title for nil product")
	}
}

func TestListingMetaStep_Name(t *testing.T) {
	s := composition.ListingMetaStep{}
	if s.Name() != "seo_meta" {
		t.Errorf("Name() = %q, want seo_meta", s.Name())
	}
}

func TestListingMetaStep_Apply(t *testing.T) {
	products := []*catalog.Product{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	ctx := composition.NewListingContext(products)

	if err := (composition.ListingMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Meta["title"] != "Products" {
		t.Errorf("title = %v, want Products", ctx.Meta["title"])
	}
	if ctx.Meta["description"] != "Browse 3 products" {
		t.Errorf("description = %v, want Browse 3 products", ctx.Meta["description"])
	}
}

func TestListingMetaStep_Apply_Empty(t *testing.T) {
	ctx := composition.NewListingContext(nil)
	if err := (composition.ListingMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Meta["description"] != "Browse 0 products" {
		t.Errorf("description = %v, want Browse 0 products", ctx.Meta["description"])
	}
}

func TestProductMetaStep_Apply_TruncatesUTF8(t *testing.T) {
	// 200 multibyte runes (each '€' is 3 bytes in UTF-8).
	long := strings.Repeat("€", 200)
	prod := catalog.Product{ID: "1", Name: "P", Description: long}
	ctx := composition.NewProductContext(&prod)

	if err := (composition.ProductMetaStep{}).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	desc, ok := ctx.Meta["description"].(string)
	if !ok {
		t.Fatal("description not a string")
	}
	if !utf8.ValidString(desc) {
		t.Error("description is not valid UTF-8 after truncation")
	}
	if utf8.RuneCountInString(desc) != 160 {
		t.Errorf("rune count = %d, want 160", utf8.RuneCountInString(desc))
	}
}
