package store_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/store"
)

func TestCountryNormalizers_UKAliasAligned(t *testing.T) {
	inputs := []string{"uk", "UK", " uk ", "Uk"}
	for _, in := range inputs {
		s, err1 := store.NormalizeCountry(in)
		o, err2 := order.ValidateDestinationCountry(in)
		if err1 != nil || err2 != nil {
			t.Fatalf("input %q: store=%v order=%v", in, err1, err2)
		}
		if s != "GB" || o != "GB" {
			t.Fatalf("input %q: store=%q order=%q, want both GB", in, s, o)
		}
	}
}
