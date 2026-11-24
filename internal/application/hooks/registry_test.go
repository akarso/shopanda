package hooks_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/hooks"
)

func TestRegistry_InvokeRunsHandlersByPriority(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var order []string
	_ = reg.Register(hooks.HookCartAddItemAfter, 200, "plugin.b", func(ctx *hooks.Context) error {
		order = append(order, "b")
		return nil
	})
	_ = reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.a", func(ctx *hooks.Context) error {
		order = append(order, "a")
		ctx.Set("seen", true)
		return nil
	})

	hookCtx := hooks.NewContext(hooks.HookCartAddItemAfter)
	if err := reg.Invoke(context.Background(), hookCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v, want [a b]", order)
	}
	if seen, ok := hookCtx.Get("seen"); !ok || seen != true {
		t.Fatalf("payload not shared across handlers: %#v", hookCtx.Payload)
	}
}

func TestRegistry_InvokeStopsOnHandlerError(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	want := errors.New("stop")
	_ = reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.a", func(ctx *hooks.Context) error {
		return want
	})
	_ = reg.Register(hooks.HookCartAddItemAfter, 200, "plugin.b", func(ctx *hooks.Context) error {
		t.Fatal("second handler should not run")
		return nil
	})

	err := reg.Invoke(context.Background(), hooks.NewContext(hooks.HookCartAddItemAfter))
	if !errors.Is(err, want) {
		t.Fatalf("Invoke err = %v, want %v", err, want)
	}
}

func TestRegistry_InvokeRecoversHandlerPanic(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.a", func(ctx *hooks.Context) error {
		panic("boom")
	})

	err := reg.Invoke(context.Background(), hooks.NewContext(hooks.HookCartAddItemAfter))
	if err == nil || err.Error() == "" {
		t.Fatalf("Invoke err = %v, want panic error", err)
	}
}

func TestRegistry_InvokeContinueOnErrorKeepsGoing(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var ran []string
	var seen []string
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "plugin.a", func(ctx *hooks.Context) error {
		ran = append(ran, "a")
		return errors.New("skip me")
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "plugin.b", func(ctx *hooks.Context) error {
		ran = append(ran, "b")
		return nil
	})

	hookCtx := hooks.NewContext(hooks.HookOrderLineDisplay)
	reg.InvokeContinueOnError(context.Background(), hookCtx, func(registrant string, err error) {
		seen = append(seen, registrant+":"+err.Error())
	})
	if len(ran) != 2 || ran[0] != "a" || ran[1] != "b" {
		t.Fatalf("ran = %v, want [a b]", ran)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], "plugin.a") {
		t.Fatalf("onErr = %v", seen)
	}
}

func TestRegistry_InvokeContinueOnError_RespectsCancel(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var ran int
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "a", func(ctx *hooks.Context) error {
		ran++
		return nil
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "b", func(ctx *hooks.Context) error {
		ran++
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got error
	var registrant string
	reg.InvokeContinueOnError(ctx, hooks.NewContext(hooks.HookOrderLineDisplay), func(regName string, err error) {
		registrant = regName
		got = err
	})
	if ran != 0 {
		t.Fatalf("ran = %d, want 0 after cancel", ran)
	}
	if registrant != "" {
		t.Fatalf("cancel must be chain-level (empty registrant), got %q", registrant)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("onErr = %v, want canceled", got)
	}
}

func TestRegistry_CatalogListsHandlers(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookCartAddItemAfter, 100, "plugin.a", func(ctx *hooks.Context) error { return nil })
	_ = reg.Register(hooks.HookCartAddItemAfter, 200, "plugin.b", func(ctx *hooks.Context) error { return nil })

	catalog := reg.Catalog()
	if len(catalog) != 1 || catalog[0].Name != hooks.HookCartAddItemAfter {
		t.Fatalf("catalog = %+v", catalog)
	}
	if len(catalog[0].Handlers) != 2 {
		t.Fatalf("handlers = %+v", catalog[0].Handlers)
	}
	if catalog[0].Handlers[0].Registrant != "plugin.a" || catalog[0].Handlers[1].Registrant != "plugin.b" {
		t.Fatalf("handler order = %+v", catalog[0].Handlers)
	}
}
