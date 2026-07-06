package plugin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/assets"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_AssetsRegister(t *testing.T) {
	reg := assets.NewRegistry()
	app := &plugin.App{}
	app.SetAssetRegistry(reg)

	err := app.Assets("plugin.demo").Register(assets.Manifest{
		Path: "/plugins/demo.js", Kind: assets.KindJS, Placement: assets.PlacementHead,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := reg.ForRoute("/"); len(got.HeadJS) != 1 {
		t.Fatalf("ForRoute() = %+v", got)
	}
}

func TestApp_SetAssetRegistry_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app := &plugin.App{}
	app.SetAssetRegistry(nil)
}
