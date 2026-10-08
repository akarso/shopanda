package checkout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestCartRequiresPhysicalShipping(t *testing.T) {
	t.Parallel()

	mustCart := func(t *testing.T, variantIDs ...string) *cart.Cart {
		t.Helper()
		c, err := cart.NewCart(id.New(), "EUR")
		if err != nil {
			t.Fatalf("NewCart: %v", err)
		}
		for _, vid := range variantIDs {
			if err := c.AddItem(vid, 1, shared.MustNewMoney(1000, "EUR")); err != nil {
				t.Fatalf("AddItem(%s): %v", vid, err)
			}
		}
		return &c
	}

	virtualProducts := &mockProductRepo047{defaultType: catalog.TypeVirtual}
	simpleProducts := &mockProductRepo047{defaultType: catalog.TypeSimple}
	variants := &mockVariantForShipping047{}

	t.Run("empty cart fails closed", func(t *testing.T) {
		c := mustCart(t)
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, simpleProducts, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true for empty cart")
		}
	})

	t.Run("nil cart fails closed", func(t *testing.T) {
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), nil, simpleProducts, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true for nil cart")
		}
	})

	t.Run("nil repos fail closed", func(t *testing.T) {
		c := mustCart(t, "v1")
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, nil, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true when products repo is nil")
		}
		got, err = checkout.CartRequiresPhysicalShipping(context.Background(), c, simpleProducts, nil, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true when variants repo is nil")
		}
	})

	t.Run("digital only virtual", func(t *testing.T) {
		c := mustCart(t, "v1", "v2")
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, virtualProducts, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got {
			t.Fatal("want false for virtual-only cart")
		}
	})

	t.Run("digital only downloadable", func(t *testing.T) {
		c := mustCart(t, "v1")
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, &mockProductRepo047{defaultType: catalog.TypeDownloadable}, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got {
			t.Fatal("want false for downloadable-only cart")
		}
	})

	t.Run("mixed requires shipping", func(t *testing.T) {
		c := mustCart(t, "v1", "v2")
		products := &mockProductRepo047{
			products: map[string]*catalog.Product{
				"prod-v1": {ID: "prod-v1", Type: catalog.TypeVirtual, Status: catalog.StatusActive, Name: "V", Slug: "v"},
				"prod-v2": {ID: "prod-v2", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "S", Slug: "s"},
			},
		}
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, products, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true for mixed cart")
		}
	})

	t.Run("missing product fails closed", func(t *testing.T) {
		c := mustCart(t, "v1")
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, &nilProductRepo047{}, variants, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Fatal("want true when product is missing")
		}
	})

	t.Run("variant lookup error", func(t *testing.T) {
		c := mustCart(t, "v1")
		got, err := checkout.CartRequiresPhysicalShipping(context.Background(), c, simpleProducts, &errVariantRepo047{err: errors.New("db down")}, nil)
		if err == nil {
			t.Fatal("expected lookup error")
		}
		if !got {
			t.Fatal("want true (fail closed) with lookup error")
		}
	})
}
