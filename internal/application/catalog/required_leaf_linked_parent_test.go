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

func TestLinkedParentChildProductIDs(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"ticket":  {ID: "ticket", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
		"bundle":  {ID: "bundle", Status: catalog.StatusActive, Type: catalog.TypeBundle},
		"child-1": {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeSimple, RequiresLinkedParent: true},
		"child-2": {ID: "child-2", Status: catalog.StatusActive, Type: catalog.TypeSimple},
	}}
	comp1, err := catalog.NewBundleComponent("bundle", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	comp2, err := catalog.NewBundleComponent("bundle", "child-2", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	components := &stubBundleComponentRepo{list: []catalog.BundleComponent{*comp1, *comp2}}

	ids, err := catalogApp.LinkedParentChildProductIDs(context.Background(), products, components, products.byID["ticket"])
	if err != nil || len(ids) != 1 || ids[0] != "ticket" {
		t.Fatalf("ticket ids = %v err=%v", ids, err)
	}
	ids, err = catalogApp.LinkedParentChildProductIDs(context.Background(), products, components, products.byID["bundle"])
	if err != nil || len(ids) != 1 || ids[0] != "child-1" {
		t.Fatalf("bundle ids = %v err=%v, want linked leaf only", ids, err)
	}
}

type stubDownloadableFileCount struct {
	n   int
	err error
}

func (s stubDownloadableFileCount) CountByProductID(context.Context, string) (int, error) {
	return s.n, s.err
}

func TestRejectRequiredMissingDownloadableFileLeaves(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"child-1": {ID: "child-1", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}}
	comp, err := catalog.NewBundleComponent("bundle-1", "child-1", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	components := &stubBundleComponentRepo{list: []catalog.BundleComponent{*comp}}

	err = catalogApp.RejectRequiredMissingDownloadableFileLeaves(context.Background(), products, components, stubDownloadableFileCount{n: 0}, "bundle-1")
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), catalog.DownloadableMissingFileNotPurchasableMessage) {
		t.Fatalf("err = %v, want missing-file validation", err)
	}

	err = catalogApp.RejectRequiredMissingDownloadableFileLeaves(context.Background(), products, components, stubDownloadableFileCount{n: 1}, "bundle-1")
	if err != nil {
		t.Fatalf("with file: %v", err)
	}

	err = catalogApp.RejectRequiredMissingDownloadableFileLeaves(context.Background(), products, components, nil, "bundle-1")
	if !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("nil counter = %v, want internal fail-closed", err)
	}
}
