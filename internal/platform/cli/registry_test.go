package cli_test

import (
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
)

func TestRegistry_RegisterAndRun(t *testing.T) {
	reg := cli.NewRegistry()
	reg.Register(cli.Command{
		Name:        "example:ping",
		Description: "Ping the example plugin",
		Run: func(_ cli.Context, args []string) error {
			if len(args) != 1 || args[0] != "ok" {
				return errors.New("unexpected args")
			}
			return nil
		},
	})

	if _, ok := reg.Get("example:ping"); !ok {
		t.Fatal("expected registered command")
	}
	if err := reg.Run("example:ping", cli.Context{}, []string{"ok"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRegistry_ListSorted(t *testing.T) {
	reg := cli.NewRegistry()
	reg.Register(cli.Command{Name: "b:cmd", Description: "b", Run: func(cli.Context, []string) error { return nil }})
	reg.Register(cli.Command{Name: "a:cmd", Description: "a", Run: func(cli.Context, []string) error { return nil }})

	list := reg.List()
	if len(list) != 2 || list[0].Name != "a:cmd" || list[1].Name != "b:cmd" {
		t.Fatalf("List() = %#v, want sorted [a:cmd b:cmd]", list)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	reg := cli.NewRegistry()
	reg.Register(cli.Command{Name: "x:y", Description: "x", Run: func(cli.Context, []string) error { return nil }})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate command")
		}
	}()
	reg.Register(cli.Command{Name: "x:y", Description: "dup", Run: func(cli.Context, []string) error { return nil }})
}
