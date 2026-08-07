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

func TestRegistry_Catalog(t *testing.T) {
	reg := slots.NewRegistry(nil)
	_ = reg.RegisterRenderer("pdp.info", slots.PlacementAppend, 100, "plugin.a", func(ctx *slots.RenderContext) string {
		return "A"
	})
	_ = reg.RegisterRenderer("pdp.info", slots.PlacementBefore, 200, "plugin.b", func(ctx *slots.RenderContext) string {
		return "B"
	})

	catalog := reg.Catalog()
	if len(catalog) != 1 || catalog[0].Name != "pdp.info" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if len(catalog[0].Handlers) != 2 {
		t.Fatalf("handlers = %#v", catalog[0].Handlers)
	}
	if catalog[0].Handlers[0].Placement != slots.PlacementBefore {
		t.Fatalf("first placement = %q", catalog[0].Handlers[0].Placement)
	}
}

func TestRegistry_DevModeWarnsForUnmarkedAnchor(t *testing.T) {
	var warned bool
	log := &captureLogger{warn: func(event string, ctx map[string]interface{}) {
		if event == "slots.registration.unmarked_anchor" {
			warned = true
		}
	}}
	reg := slots.NewRegistry(log)
	reg.SetDevMode(true)
	reg.SetThemeMarkers([]string{"pdp.info"})

	_ = reg.RegisterRenderer("missing.anchor", slots.PlacementAppend, 100, "plugin.a", func(ctx *slots.RenderContext) string {
		return ""
	})
	if !warned {
		t.Fatal("expected dev warning for unmarked anchor")
	}

	warned = false
	_ = reg.RegisterRenderer("pdp.info", slots.PlacementAppend, 100, "plugin.b", func(ctx *slots.RenderContext) string {
		return ""
	})
	if warned {
		t.Fatal("did not expect warning for marked anchor")
	}
}

type captureLogger struct {
	warn func(event string, ctx map[string]interface{})
}

func (l *captureLogger) Info(string, map[string]interface{}) {}
func (l *captureLogger) Warn(event string, ctx map[string]interface{}) {
	if l.warn != nil {
		l.warn(event, ctx)
	}
}
func (l *captureLogger) Error(string, error, map[string]interface{}) {}
