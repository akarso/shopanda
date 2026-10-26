package catalog_test

import (
	"context"
	"strings"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestRejectRequiredLinkedParentLeaves(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"child-1": {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	components := &stubBundleComponentRepo{list: []catalog.BundleComponent{*comp}}

	err = catalogApp.RejectRequiredLinkedParentLeaves(context.Background(), products, components, "bundle-1", catalog.AssignmentContext{})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.RequiresLinkedParentNotPurchasableMessage) {
		t.Fatalf("err = %v, want linked-parent validation", err)
	}

	err = catalogApp.RejectRequiredLinkedParentLeaves(context.Background(), products, components, "bundle-1", catalog.AssignmentContext{Deferred: true})
	if err != nil {
		t.Fatalf("deferred assignment: %v", err)
	}
}
