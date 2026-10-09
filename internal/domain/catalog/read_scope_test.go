package catalog_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestIncludeNonActiveProducts_DefaultFalse(t *testing.T) {
	if catalog.IncludeNonActiveProducts(context.Background()) {
		t.Fatal("expected false without scope marker")
	}
}

func TestWithIncludeNonActiveProducts(t *testing.T) {
	ctx := catalog.WithIncludeNonActiveProducts(context.Background())
	if !catalog.IncludeNonActiveProducts(ctx) {
		t.Fatal("expected true after WithIncludeNonActiveProducts")
	}
}

func TestBypassProductVisibility_DefaultFalse(t *testing.T) {
	if catalog.BypassProductVisibility(context.Background()) {
		t.Fatal("expected false without bypass marker")
	}
}

func TestWithBypassProductVisibility(t *testing.T) {
	ctx := catalog.WithBypassProductVisibility(context.Background())
	if !catalog.BypassProductVisibility(ctx) {
		t.Fatal("expected true after WithBypassProductVisibility")
	}
	if catalog.IncludeNonActiveProducts(ctx) {
		t.Fatal("visibility bypass must not imply include-non-active")
	}
}

func TestWithOperatorProductReadScope(t *testing.T) {
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	if !catalog.IncludeNonActiveProducts(ctx) {
		t.Fatal("expected include-non-active")
	}
	if !catalog.BypassProductVisibility(ctx) {
		t.Fatal("expected visibility bypass")
	}
}

func TestActiveForPurchase(t *testing.T) {
	if catalog.ActiveForPurchase(nil) {
		t.Fatal("nil product")
	}
	draft := &catalog.Product{Status: catalog.StatusDraft}
	if catalog.ActiveForPurchase(draft) {
		t.Fatal("draft")
	}
	active := &catalog.Product{Status: catalog.StatusActive}
	if !catalog.ActiveForPurchase(active) {
		t.Fatal("active")
	}
}
