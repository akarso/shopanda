package plugin_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type cliTestPlugin struct{}

func (cliTestPlugin) Name() string { return "test/cli" }

func (cliTestPlugin) Init(app *plugin.App) error {
	app.RegisterCommand(cli.Command{
		Name:        "test:cli",
		Description: "Test CLI registration",
		Run:         func(cli.Context, []string) error { return nil },
	})
	return nil
}

func TestApp_RegisterCommandViaInit(t *testing.T) {
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(cliTestPlugin{})
	reg.InitAll(&plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")})

	if _, ok := reg.CLIRegistry().Get("test:cli"); !ok {
		t.Fatal("expected test:cli to be registered")
	}
}
