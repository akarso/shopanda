package plugin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_SlotsRegisterRenderer(t *testing.T) {
	reg := slots.NewRegistry(nil)
	app := &plugin.App{}
	app.SetSlotRegistry(reg)

	err := app.Slots("plugin.demo").RegisterRenderer("pdp.price", slots.PlacementAppend, 100, func(ctx *slots.RenderContext) string {
		return "<span>eco</span>"
	})
	if err != nil {
		t.Fatalf("RegisterRenderer: %v", err)
	}
	if got := reg.Render("pdp.price", slots.PlacementAppend, nil); got != "<span>eco</span>" {
		t.Fatalf("Render() = %q", got)
	}
}

func TestApp_SetSlotRegistry_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app := &plugin.App{}
	app.SetSlotRegistry(nil)
}
