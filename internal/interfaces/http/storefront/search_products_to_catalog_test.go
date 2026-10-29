package storefront

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
)

func TestSearchProductsToCatalog_CopiesType(t *testing.T) {
	got := searchProductsToCatalog([]search.Product{
		{ID: "p1", Name: "Bundle", Type: string(catalog.TypeBundle)},
	})
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Type != catalog.TypeBundle {
		t.Fatalf("Type = %q, want %q", got[0].Type, catalog.TypeBundle)
	}
}

func TestSearchProductsToCatalog_CopiesStatus(t *testing.T) {
	got := searchProductsToCatalog([]search.Product{
		{ID: "p1", Name: "Draft", Status: "draft"},
		{ID: "p2", Name: "Empty"},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Status != catalog.StatusDraft {
		t.Fatalf("Status = %q, want draft", got[0].Status)
	}
	if got[1].Status != catalog.StatusActive {
		t.Fatalf("empty Status = %q, want active default", got[1].Status)
	}
}
