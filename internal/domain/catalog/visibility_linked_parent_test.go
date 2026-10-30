package catalog_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func linkedChild(flag bool, purchasable catalog.VisibilityMode) catalog.Product {
	p, err := catalog.NewProduct("child", "Ticket", "ticket")
	if err != nil {
		panic(err)
	}
	p.Status = catalog.StatusActive
	p.RequiresLinkedParent = flag
	modes := catalog.DefaultVisibilityAxes()
	modes.Catalog = catalog.VisibilityModeVisible
	modes.Search = catalog.VisibilityModeVisible
	modes.Individually = catalog.VisibilityModeVisible
	modes.Purchasable = purchasable
	p.VisibilityModes = modes
	return p
}

func sellableInputs() catalog.VisibilityInputs {
	return catalog.VisibilityInputs{
		QuantityPositive:   true,
		HasPrice:           true,
		HasStoreAssignment: true,
	}
}

func TestVisibility_RequiresLinkedParent_EmptyBlocksPurchasableOnly(t *testing.T) {
	p := linkedChild(true, catalog.VisibilityModeAuto)
	got := p.Visibility(sellableInputs(), catalog.VisibilityOptions{})
	if got.Purchasable {
		t.Fatal("Purchasable = true, want false when assignment is empty")
	}
	if !got.VisibleInCatalog || !got.VisibleInSearch || !got.VisibleIndividually {
		t.Fatalf("display axes = %+v, want true", got)
	}
}

func TestVisibility_RequiresLinkedParent_VisibleDoesNotBypass(t *testing.T) {
	p := linkedChild(true, catalog.VisibilityModeVisible)
	got := p.Visibility(sellableInputs(), catalog.VisibilityOptions{})
	if got.Purchasable {
		t.Fatal("Purchasable=visible must not bypass an empty assignment context")
	}
	if !got.VisibleInCatalog {
		t.Fatal("catalog axis should stay visible")
	}
}

func TestVisibility_RequiresLinkedParent_ParentOrDeferredRestores(t *testing.T) {
	p := linkedChild(true, catalog.VisibilityModeAuto)
	opts := catalog.VisibilityOptions{}

	withParent := sellableInputs()
	withParent.Assignment = catalog.AssignmentContext{ParentProductID: "  card-1 "}
	if !p.Visibility(withParent, opts).Purchasable {
		t.Fatal("parent id should restore Purchasable")
	}

	deferred := sellableInputs()
	deferred.Assignment = catalog.AssignmentContext{Deferred: true}
	if !p.Visibility(deferred, opts).Purchasable {
		t.Fatal("deferred assignment should restore Purchasable")
	}

	blank := sellableInputs()
	blank.Assignment = catalog.AssignmentContext{ParentProductID: "   "}
	if p.Visibility(blank, opts).Purchasable {
		t.Fatal("whitespace parent should stay not purchasable")
	}
}

func TestVisibility_RequiresLinkedParent_OffLeavesPurchasable(t *testing.T) {
	p := linkedChild(false, catalog.VisibilityModeAuto)
	if !p.Visibility(sellableInputs(), catalog.VisibilityOptions{}).Purchasable {
		t.Fatal("flag false should leave a sellable auto axis purchasable")
	}
}

func TestVisibility_RequiresLinkedParent_IgnoresSpoofedInputFlag(t *testing.T) {
	p := linkedChild(true, catalog.VisibilityModeAuto)
	in := sellableInputs()
	in.RequiresLinkedParent = false
	if p.Visibility(in, catalog.VisibilityOptions{}).Purchasable {
		t.Fatal("caller-supplied RequiresLinkedParent=false must not unblock a flagged product")
	}

	off := linkedChild(false, catalog.VisibilityModeAuto)
	in.RequiresLinkedParent = true
	if !off.Visibility(in, catalog.VisibilityOptions{}).Purchasable {
		t.Fatal("caller-supplied RequiresLinkedParent=true must not block an unflagged product")
	}
}

func TestVisibilitySnapshot_ResolvePrefersSnapshotFlag(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeVisible
	snap := catalog.VisibilitySnapshot{
		Status: catalog.StatusActive,
		Modes:  modes,
		Inputs: catalog.VisibilityInputs{
			QuantityPositive:     true,
			HasPrice:             true,
			HasStoreAssignment:   true,
			RequiresLinkedParent: false,
		},
		RequiresLinkedParent: true,
	}
	if snap.Resolve(catalog.VisibilityOptions{}).Purchasable {
		t.Fatal("stale Inputs.RequiresLinkedParent=false must not unblock snap.RequiresLinkedParent")
	}

	snap.RequiresLinkedParent = false
	snap.Inputs.RequiresLinkedParent = true
	if !snap.Resolve(catalog.VisibilityOptions{}).Purchasable {
		t.Fatal("stale Inputs.RequiresLinkedParent=true must not block snap.RequiresLinkedParent=false")
	}
}

func TestExplainAxes_RequiresLinkedParentCodeOnlyOnPurchasable(t *testing.T) {
	p := linkedChild(true, catalog.VisibilityModeVisible)
	in := sellableInputs()
	in.RequiresLinkedParent = true
	autoBasis, codes, axes := catalog.ExplainAxes(p.VisibilityModes, p.Status, in, catalog.VisibilityOptions{})
	if !autoBasis {
		t.Fatal("auto basis should stay true; the gate is not an AutoBasis check")
	}
	for _, code := range codes {
		if code == catalog.RequiresLinkedParentCode {
			t.Fatalf("shared AutoBasis codes = %v, must not include %s", codes, catalog.RequiresLinkedParentCode)
		}
	}
	if got := catalog.AutoBasisFailureCodes(p.Status, in, catalog.VisibilityOptions{}); len(got) != 0 {
		t.Fatalf("AutoBasisFailureCodes = %v, want empty", got)
	}
	if axes.Purchasable.Resolved || axes.Purchasable.Effective {
		t.Fatalf("purchasable explanation = %+v, want false/false", axes.Purchasable)
	}
	if len(axes.Purchasable.FailureCodes) != 1 || axes.Purchasable.FailureCodes[0] != catalog.RequiresLinkedParentCode {
		t.Fatalf("purchasable codes = %v", axes.Purchasable.FailureCodes)
	}
	if len(axes.Catalog.FailureCodes) != 0 {
		t.Fatalf("catalog codes = %v, want none", axes.Catalog.FailureCodes)
	}
	if !axes.Catalog.Resolved || !axes.Catalog.Effective {
		t.Fatalf("catalog explanation = %+v, want visible", axes.Catalog)
	}
}

func TestFilterIDsWithFacts_RequiresLinkedParentPurchasableOnly(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	modes.Purchasable = catalog.VisibilityModeVisible
	snap := sellableSnap(catalog.StatusActive, modes)
	snap.RequiresLinkedParent = true
	facts := mapFacts{"child": snap}
	ctx := context.Background()

	catalogIDs, err := catalog.FilterIDsWithFacts(ctx, []string{"child"}, catalog.VisibilitySurfaceCatalog, facts, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatalf("catalog filter: %v", err)
	}
	if len(catalogIDs) != 1 {
		t.Fatalf("catalog ids = %v, want the product (display axis unchanged)", catalogIDs)
	}

	purchaseIDs, err := catalog.FilterIDsWithFacts(ctx, []string{"child"}, catalog.VisibilitySurfacePurchasable, facts, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatalf("purchasable filter: %v", err)
	}
	if len(purchaseIDs) != 0 {
		t.Fatalf("purchasable ids = %v, want none", purchaseIDs)
	}

	withParent := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-1"})
	purchaseIDs, err = catalog.FilterIDsWithFacts(withParent, []string{"child"}, catalog.VisibilitySurfacePurchasable, facts, catalog.VisibilityOptions{})
	if err != nil {
		t.Fatalf("purchasable filter with parent: %v", err)
	}
	if len(purchaseIDs) != 1 || purchaseIDs[0] != "child" {
		t.Fatalf("purchasable ids = %v, want [child]", purchaseIDs)
	}
}
