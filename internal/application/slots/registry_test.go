package slots_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
)

func TestRegistry_RenderOrdersByPriority(t *testing.T) {
	reg := slots.NewRegistry(nil)
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementAppend, 200, "plugin.b", func(ctx *slots.RenderContext) string {
		return "<span>B</span>"
	})
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementAppend, 100, "plugin.a", func(ctx *slots.RenderContext) string {
		return "<span>A</span>"
	})

	got := reg.Render("pdp.price", slots.PlacementAppend, nil)
	if got != "<span>A</span><span>B</span>" {
		t.Fatalf("Render() = %q, want A then B", got)
	}
}

func TestRegistry_RegisterRendererTrimsAnchorForLookup(t *testing.T) {
	reg := slots.NewRegistry(nil)
	if err := reg.RegisterRenderer(" pdp.price ", slots.PlacementAppend, 100, "plugin.a", func(ctx *slots.RenderContext) string {
		return "<span>hit</span>"
	}); err != nil {
		t.Fatalf("RegisterRenderer: %v", err)
	}

	got := reg.Render("pdp.price", slots.PlacementAppend, nil)
	if got != "<span>hit</span>" {
		t.Fatalf("Render() = %q, want trimmed anchor lookup", got)
	}
}

func TestRegistry_RenderUnknownAnchorNoop(t *testing.T) {
	reg := slots.NewRegistry(nil)
	if got := reg.Render("missing.anchor", slots.PlacementBefore, nil); got != "" {
		t.Fatalf("Render() = %q, want empty", got)
	}
}

func TestRegistry_RenderRecoversRendererPanic(t *testing.T) {
	reg := slots.NewRegistry(nil)
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementBefore, 100, "plugin.a", func(ctx *slots.RenderContext) string {
		panic("boom")
	})
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementBefore, 200, "plugin.b", func(ctx *slots.RenderContext) string {
		return "<ok/>"
	})

	got := reg.Render("pdp.price", slots.PlacementBefore, nil)
	if got != "<ok/>" {
		t.Fatalf("Render() = %q, want surviving renderer output", got)
	}
}

func TestValidateAnchorName(t *testing.T) {
	if err := slots.ValidateAnchorName("pdp.price"); err != nil {
		t.Fatalf("ValidateAnchorName: %v", err)
	}
	if err := slots.ValidateAnchorName(""); err == nil {
		t.Fatal("expected error for empty anchor")
	}
	if err := slots.ValidateAnchorName("Bad.Anchor"); err == nil {
		t.Fatal("expected error for invalid anchor")
	}
}
