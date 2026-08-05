package search_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/search"
)

func TestReservedFacetKey(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"category", "category_id"} {
		if !search.ReservedFacetKey(code) {
			t.Fatalf("ReservedFacetKey(%q) = false, want true", code)
		}
	}
	if search.ReservedFacetKey("color") {
		t.Fatal("ReservedFacetKey(color) = true, want false")
	}
}
