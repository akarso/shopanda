package catalog_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestAssignmentContext_Empty(t *testing.T) {
	if !(catalog.AssignmentContext{}).Empty() {
		t.Fatal("zero context should be empty")
	}
	if !(catalog.AssignmentContext{ParentProductID: "  \t"}).Empty() {
		t.Fatal("whitespace parent should be empty")
	}
	if (catalog.AssignmentContext{ParentProductID: " card "}).Empty() {
		t.Fatal("parent id should not be empty")
	}
	if (catalog.AssignmentContext{Deferred: true}).Empty() {
		t.Fatal("deferred should not be empty")
	}
	if (catalog.AssignmentContext{ParentProductID: "  ", Deferred: true}).Empty() {
		t.Fatal("deferred with whitespace parent should not be empty")
	}
}

func TestAssignmentContextFrom_MissingIsEmpty(t *testing.T) {
	if !catalog.AssignmentContextFrom(nil).Empty() {
		t.Fatal("nil ctx should be empty")
	}
	if !catalog.AssignmentContextFrom(context.Background()).Empty() {
		t.Fatal("missing value should be empty")
	}
}

func TestWithAssignmentContext_TrimsParent(t *testing.T) {
	ctx := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: "  card-1  "})
	got := catalog.AssignmentContextFrom(ctx)
	if got.Empty() || got.ParentProductID != "card-1" {
		t.Fatalf("got %+v, want parent card-1", got)
	}

	blank := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{ParentProductID: " \n "})
	if !catalog.AssignmentContextFrom(blank).Empty() {
		t.Fatal("whitespace parent stored on ctx should read back empty")
	}

	deferred := catalog.WithAssignmentContext(nil, catalog.AssignmentContext{Deferred: true, ParentProductID: "  "})
	got = catalog.AssignmentContextFrom(deferred)
	if got.Empty() || !got.Deferred {
		t.Fatalf("got %+v, want deferred", got)
	}
}

func TestResolvePurchaseAssignment_LineWinsThenRequest(t *testing.T) {
	line := catalog.AssignmentContext{ParentProductID: "card-line"}
	req := catalog.WithAssignmentContext(context.Background(), catalog.AssignmentContext{Deferred: true})
	got := catalog.ResolvePurchaseAssignment(req, line)
	if got.ParentProductID != "card-line" || got.Deferred {
		t.Fatalf("got %+v, want line parent", got)
	}

	emptyLine := catalog.ResolvePurchaseAssignment(req, catalog.AssignmentContext{})
	if emptyLine.Empty() || !emptyLine.Deferred {
		t.Fatalf("got %+v, want request deferred", emptyLine)
	}

	if !catalog.ResolvePurchaseAssignment(context.Background(), catalog.AssignmentContext{}).Empty() {
		t.Fatal("both empty should stay empty")
	}
}

func TestLinkedParentBlocksPurchase(t *testing.T) {
	if catalog.LinkedParentBlocksPurchase(nil, catalog.AssignmentContext{}) {
		t.Fatal("nil product must not block")
	}
	off := &catalog.Product{RequiresLinkedParent: false}
	if catalog.LinkedParentBlocksPurchase(off, catalog.AssignmentContext{}) {
		t.Fatal("flag false must not block")
	}
	on := &catalog.Product{RequiresLinkedParent: true}
	if !catalog.LinkedParentBlocksPurchase(on, catalog.AssignmentContext{}) {
		t.Fatal("flag true + empty assignment must block")
	}
	if catalog.LinkedParentBlocksPurchase(on, catalog.AssignmentContext{Deferred: true}) {
		t.Fatal("deferred must not block")
	}
	if catalog.LinkedParentBlocksPurchase(on, catalog.AssignmentContext{ParentProductID: "card-1"}) {
		t.Fatal("parent id must not block")
	}
}
