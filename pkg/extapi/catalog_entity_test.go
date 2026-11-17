package extapi_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestClassifyProductSlug(t *testing.T) {
	cases := []struct {
		name string
		v    *extapi.CatalogVariant
		want string
		cls  extapi.ProductSlugClass
	}{
		{name: "nil", want: "shirt", cls: extapi.ProductSlugUnknown},
		{name: "not visible", v: &extapi.CatalogVariant{}, want: "shirt", cls: extapi.ProductSlugUnknown},
		{name: "visible blank slug", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: ""}, want: "shirt", cls: extapi.ProductSlugUnknown},
		{name: "visible whitespace slug", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "  "}, want: "shirt", cls: extapi.ProductSlugUnknown},
		{name: "match", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "shirt"}, want: "shirt", cls: extapi.ProductSlugMatch},
		{name: "other product", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "other"}, want: "shirt", cls: extapi.ProductSlugOther},
		{name: "not visible with slug still unknown", v: &extapi.CatalogVariant{ProductVisible: false, ProductSlug: "shirt"}, want: "shirt", cls: extapi.ProductSlugUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extapi.ClassifyProductSlug(tc.v, tc.want)
			if got != tc.cls {
				t.Fatalf("ClassifyProductSlug = %v, want %v", got, tc.cls)
			}
			if match := extapi.BelongsToProductSlug(tc.v, tc.want); match != (tc.cls == extapi.ProductSlugMatch) {
				t.Fatalf("BelongsToProductSlug = %v, want match-only", match)
			}
		})
	}
}
