package catalog_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewDownloadableFile_TrimsAndDefaults(t *testing.T) {
	f, err := catalog.NewDownloadableFile("  prod-1  ", "  asset-1  ")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	if f.ProductID != "prod-1" || f.AssetID != "asset-1" {
		t.Fatalf("ids = %q/%q", f.ProductID, f.AssetID)
	}
	if f.Position != 0 || f.Label != "" {
		t.Fatalf("defaults = position %d label %q", f.Position, f.Label)
	}
}

func TestDownloadableFile_Validate(t *testing.T) {
	ok, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*catalog.DownloadableFile)
		want string
	}{
		{"empty product", func(f *catalog.DownloadableFile) { f.ProductID = "  " }, "product id"},
		{"empty asset", func(f *catalog.DownloadableFile) { f.AssetID = "" }, "asset id"},
		{"negative position", func(f *catalog.DownloadableFile) { f.Position = -1 }, "position"},
		{"position too high", func(f *catalog.DownloadableFile) { f.Position = 10000 }, "9999"},
		{"label too long", func(f *catalog.DownloadableFile) { f.Label = strings.Repeat("x", 201) }, "label"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := *ok
			tc.mut(&f)
			err := f.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDownloadableFile_Validate_TrimsIDsInPlace(t *testing.T) {
	f := &catalog.DownloadableFile{ProductID: "  p1  ", AssetID: "  a1  "}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if f.ProductID != "p1" || f.AssetID != "a1" {
		t.Fatalf("ids after Validate = %q/%q", f.ProductID, f.AssetID)
	}
}

func TestDownloadableFile_ValidateAttachment(t *testing.T) {
	f, err := catalog.NewDownloadableFile("p1", "a1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := f.ValidateAttachment(catalog.TypeDownloadable); err != nil {
		t.Fatalf("downloadable: %v", err)
	}
	err = f.ValidateAttachment(catalog.TypeSimple)
	if err == nil || !strings.Contains(err.Error(), "downloadable") {
		t.Fatalf("simple = %v, want type error", err)
	}
	err = f.ValidateAttachment(catalog.TypeVirtual)
	if err == nil || !strings.Contains(err.Error(), string(catalog.TypeDownloadable)) {
		t.Fatalf("virtual = %v", err)
	}
}

func TestDownloadableMissingFileBlocksPurchase(t *testing.T) {
	if catalog.DownloadableMissingFileBlocksPurchase(nil, 0) {
		t.Fatal("nil product must not block")
	}
	simple := &catalog.Product{Type: catalog.TypeSimple}
	if catalog.DownloadableMissingFileBlocksPurchase(simple, 0) {
		t.Fatal("simple must not block")
	}
	dl := &catalog.Product{Type: catalog.TypeDownloadable}
	if !catalog.DownloadableMissingFileBlocksPurchase(dl, 0) {
		t.Fatal("downloadable with 0 files must block")
	}
	if catalog.DownloadableMissingFileBlocksPurchase(dl, 1) {
		t.Fatal("downloadable with files must not block")
	}
}

func TestRequiresPhysicalShipping_Downloadable(t *testing.T) {
	if catalog.TypeDownloadable.RequiresPhysicalShipping() {
		t.Fatal("downloadable must not require physical shipping (PR-1050)")
	}
	p := &catalog.Product{Type: catalog.TypeDownloadable}
	if p.RequiresPhysicalShipping() {
		t.Fatal("product downloadable must not require physical shipping")
	}
}

func TestDownloadableFile_Validate_TrimsLabel(t *testing.T) {
	f := &catalog.DownloadableFile{ProductID: "p1", AssetID: "a1", Label: "  Manual  "}
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if f.Label != "Manual" {
		t.Fatalf("label = %q, want trimmed", f.Label)
	}
}
