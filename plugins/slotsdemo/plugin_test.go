package slotsdemo_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/slotsdemo"
)

func testApp(t *testing.T, enabled bool) *plugin.App {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	reg := slots.NewRegistry(log)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				SlotsDemo: config.SlotsDemoPluginConfig{Enabled: enabled},
			},
		},
	}
	app.SetSlotRegistry(reg)
	return app
}

func TestPlugin_Name(t *testing.T) {
	if got := slotsdemo.New().Name(); got != "slotsdemo/reference" {
		t.Fatalf("Name() = %q, want slotsdemo/reference", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	if err := slotsdemo.New().Init(testApp(t, false)); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersSlotRenderers(t *testing.T) {
	app := testApp(t, true)
	if err := slotsdemo.New().Init(app); err != nil {
		t.Fatalf("Init(): %v", err)
	}

	reg := app.SlotRegistry()
	t.Run("layout.footer", func(t *testing.T) {
		if got := reg.Render(string(extapi.SlotLayoutFooter), slots.PlacementAppend, nil); got == "" {
			t.Fatal("expected layout.footer renderer output")
		}
	})
	t.Run("pdp.info", func(t *testing.T) {
		if got := reg.Render(string(extapi.SlotPDPInfo), slots.PlacementAppend, nil); got == "" {
			t.Fatal("expected pdp.info renderer output")
		}
	})
}
