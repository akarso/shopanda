package catalog_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestTaxClassCode(t *testing.T) {
	tests := []struct {
		typ  catalog.Type
		want string
	}{
		{catalog.TypeSimple, "standard"},
		{catalog.TypeVirtual, "standard"},
		{catalog.TypeBundle, "standard"},
		{catalog.TypeGrouped, "standard"},
		{catalog.TypeConfigurable, "standard"},
		{catalog.TypeDownloadable, "digital"},
		{catalog.Type(""), "standard"},
		{catalog.Type("unknown"), "standard"},
	}
	for _, tt := range tests {
		if got := catalog.TaxClassCode(tt.typ); got != tt.want {
			t.Errorf("TaxClassCode(%q) = %q, want %q", tt.typ, got, tt.want)
		}
	}
	// AllTypes coverage — every recognised type must map explicitly above.
	for _, typ := range catalog.AllTypes() {
		got := catalog.TaxClassCode(typ)
		switch typ {
		case catalog.TypeDownloadable:
			if got != "digital" {
				t.Errorf("AllTypes TaxClassCode(%q) = %q, want digital", typ, got)
			}
		default:
			if got != "standard" {
				t.Errorf("AllTypes TaxClassCode(%q) = %q, want standard", typ, got)
			}
		}
	}
}
