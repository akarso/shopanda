package search_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
)

func TestAttributeFilterKey(t *testing.T) {
	if got := search.AttributeFilterKey("color"); got != "attr_color" {
		t.Fatalf("AttributeFilterKey(color) = %q, want attr_color", got)
	}
}

func TestAttributeCodeFromFilterKey(t *testing.T) {
	code, ok := search.AttributeCodeFromFilterKey("attr_brand")
	if !ok || code != "brand" {
		t.Fatalf("AttributeCodeFromFilterKey = %q, %v, want brand, true", code, ok)
	}
	if _, ok := search.AttributeCodeFromFilterKey("category"); ok {
		t.Fatal("expected false for non-prefixed key")
	}
}

func TestAttributeCodeValid(t *testing.T) {
	if !search.AttributeCodeValid("color") {
		t.Fatal("color should be valid")
	}
	if search.AttributeCodeValid("Category") {
		t.Fatal("uppercase should be invalid")
	}
	if search.AttributeCodeValid("category") && search.ReservedFacetKey("category") {
		// reserved codes can still be syntactically valid
	}
}
