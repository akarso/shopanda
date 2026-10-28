package catalog_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func downloadableProduct(purchasable catalog.VisibilityMode) catalog.Product {
	p, err := catalog.NewProduct("dl-1", "Ebook", "ebook")
	if err != nil {
		panic(err)
	}
	p.Status = catalog.StatusActive
	p.Type = catalog.TypeDownloadable
	modes := catalog.DefaultVisibilityAxes()
	modes.Catalog = catalog.VisibilityModeVisible
	modes.Search = catalog.VisibilityModeVisible
	modes.Individually = catalog.VisibilityModeVisible
	modes.Purchasable = purchasable
	p.VisibilityModes = modes
	return p
}

func TestVisibility_MissingDownloadableFile_BlocksPurchasableOnly(t *testing.T) {
	p := downloadableProduct(catalog.VisibilityModeAuto)
	in := sellableInputs()
	in.MissingDownloadableFile = true
	got := p.Visibility(in, catalog.VisibilityOptions{})
	if got.Purchasable {
		t.Fatal("Purchasable = true, want false when file is missing")
	}
	if !got.VisibleInCatalog || !got.VisibleInSearch || !got.VisibleIndividually {
		t.Fatalf("display axes = %+v, want true", got)
	}
}

func TestVisibility_MissingDownloadableFile_VisibleDoesNotBypass(t *testing.T) {
	p := downloadableProduct(catalog.VisibilityModeVisible)
	in := sellableInputs()
	in.MissingDownloadableFile = true
	got := p.Visibility(in, catalog.VisibilityOptions{})
	if got.Purchasable {
		t.Fatal("Purchasable=visible must not bypass a missing downloadable file")
	}
}

func TestVisibility_MissingDownloadableFile_OffLeavesPurchasable(t *testing.T) {
	p := downloadableProduct(catalog.VisibilityModeAuto)
	got := p.Visibility(sellableInputs(), catalog.VisibilityOptions{})
	if !got.Purchasable {
		t.Fatal("zero MissingDownloadableFile should leave Purchasable true")
	}
}

func TestExplainAxes_MissingDownloadableFileCodeOnlyOnPurchasable(t *testing.T) {
	p := downloadableProduct(catalog.VisibilityModeAuto)
	in := sellableInputs()
	in.MissingDownloadableFile = true
	_, codes, axes := catalog.ExplainAxes(p.VisibilityModes, p.Status, in, catalog.VisibilityOptions{})
	for _, code := range codes {
		if code == catalog.MissingDownloadableFileCode {
			t.Fatalf("shared AutoBasis codes = %v, must not include %s", codes, catalog.MissingDownloadableFileCode)
		}
	}
	for _, code := range axes.Catalog.FailureCodes {
		if code == catalog.MissingDownloadableFileCode {
			t.Fatalf("catalog axis must not include %s", catalog.MissingDownloadableFileCode)
		}
	}
	found := false
	for _, code := range axes.Purchasable.FailureCodes {
		if code == catalog.MissingDownloadableFileCode {
			found = true
		}
	}
	if !found {
		t.Fatalf("purchasable FailureCodes = %v, want %s", axes.Purchasable.FailureCodes, catalog.MissingDownloadableFileCode)
	}
	if axes.Purchasable.Effective {
		t.Fatal("purchasable Effective should be false")
	}
}
