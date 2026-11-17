package extapi_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestBelongsToProductSlug(t *testing.T) {
	cases := []struct {
		name string
		v    *extapi.CatalogVariant
		want string
		ok   bool
	}{
		{name: "nil", want: "shirt", ok: false},
		{name: "not visible empty slug", v: &extapi.CatalogVariant{}, want: "shirt", ok: false},
		{name: "visible blank slug fail closed", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: ""}, want: "shirt", ok: false},
		{name: "visible blank want also fail closed", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "  "}, want: "shirt", ok: false},
		{name: "match", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "shirt"}, want: "shirt", ok: true},
		{name: "mismatch", v: &extapi.CatalogVariant{ProductVisible: true, ProductSlug: "other"}, want: "shirt", ok: false},
		{name: "not visible with slug", v: &extapi.CatalogVariant{ProductVisible: false, ProductSlug: "shirt"}, want: "shirt", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extapi.BelongsToProductSlug(tc.v, tc.want); got != tc.ok {
				t.Fatalf("BelongsToProductSlug = %v, want %v", got, tc.ok)
			}
		})
	}
}
