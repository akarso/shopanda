package assets_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/assets"
)

func TestRegistry_ForRouteGatesByRoute(t *testing.T) {
	reg := assets.NewRegistry()
	if err := reg.Register("plugin.cart", assets.Manifest{
		Path: "/plugins/cart.js", Kind: assets.KindJS, Placement: assets.PlacementFooter,
		Routes: []string{"/cart"}, Priority: 100,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got := reg.ForRoute("/products"); len(got.FooterJS) != 0 {
		t.Fatalf("ForRoute(/products) = %+v, want no assets", got)
	}
	if got := reg.ForRoute("/cart"); len(got.FooterJS) != 1 || got.FooterJS[0].URL != "/plugins/cart.js" {
		t.Fatalf("ForRoute(/cart) = %+v", got)
	}
}

func TestRegistry_RegisterOrdersByPriority(t *testing.T) {
	reg := assets.NewRegistry()
	_ = reg.Register("plugin.b", assets.Manifest{
		Path: "/b.js", Kind: assets.KindJS, Placement: assets.PlacementHead, Priority: 200,
	})
	_ = reg.Register("plugin.a", assets.Manifest{
		Path: "/a.js", Kind: assets.KindJS, Placement: assets.PlacementHead, Priority: 100,
	})

	got := reg.ForRoute("/")
	if len(got.HeadJS) != 2 {
		t.Fatalf("HeadJS len = %d, want 2", len(got.HeadJS))
	}
	if got.HeadJS[0].URL != "/a.js" || got.HeadJS[1].URL != "/b.js" {
		t.Fatalf("HeadJS order = %+v, want a then b", got.HeadJS)
	}
}

func TestRegistry_RegisterDedupesSameAsset(t *testing.T) {
	reg := assets.NewRegistry()
	manifest := assets.Manifest{
		Path: "/dup.js", Kind: assets.KindJS, Placement: assets.PlacementHead, Priority: 100,
	}
	if err := reg.Register("plugin.demo", manifest); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register("plugin.demo", manifest); err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if got := reg.ForRoute("/"); len(got.HeadJS) != 1 {
		t.Fatalf("HeadJS len = %d, want 1 after duplicate register", len(got.HeadJS))
	}
}
