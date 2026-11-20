package pluginsdk_test

import (
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestRegisterCommand_PanicsOnUnsupportedNeeds(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	sdk := pluginsdk.New(&plugin.App{Logger: log}, "test/cli-needs")

	defer func() {
		rv := recover()
		if rv == nil {
			t.Fatal("expected panic for unsupported CLICommand.Needs")
		}
		msg, _ := rv.(string)
		if !strings.Contains(msg, "unsupported") || !strings.Contains(msg, "CapCatalogSeed") {
			t.Fatalf("panic = %v, want unsupported CapCatalogSeed message", rv)
		}
	}()
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:  "test:bad-need",
		Needs: []pluginsdk.Capability{"catalog.seed.typo"},
		Run:   func(pluginsdk.CLIContext, []string) error { return nil },
	})
}

func TestRegisterCommand_PanicsOnEmptyNeeds(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	sdk := pluginsdk.New(&plugin.App{Logger: log}, "test/cli-empty-need")

	defer func() {
		rv := recover()
		if rv == nil {
			t.Fatal("expected panic for empty CLICommand.Needs entry")
		}
		msg, _ := rv.(string)
		if !strings.Contains(msg, "empty") {
			t.Fatalf("panic = %v, want empty capability message", rv)
		}
	}()
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:  "test:empty-need",
		Needs: []pluginsdk.Capability{""},
		Run:   func(pluginsdk.CLIContext, []string) error { return nil },
	})
}
