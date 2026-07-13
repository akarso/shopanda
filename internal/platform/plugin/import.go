package plugin

import (
	"fmt"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Import exposes import row hook registration to plugins during Init.
type Import struct {
	registry   *importctx.Registry
	registrant string
}

// RegisterRowHook adds a handler for entity at priority (lower runs first).
func (i *Import) RegisterRowHook(entity extapi.ImportEntity, priority int, handler extapi.ImportRowHandler) error {
	if i == nil || i.registry == nil {
		return fmt.Errorf("plugin: import registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: import row handler must not be nil")
	}
	return i.registry.Register(string(entity), priority, i.registrant, func(ctx *importctx.RowContext) error {
		extCtx := toExtAPIImportRowContext(ctx)
		if err := handler(extCtx); err != nil {
			return err
		}
		syncImportRowContext(ctx, extCtx)
		return nil
	})
}

// SetImportRegistry wires the shared import row hook registry before plugin Init.
func (a *App) SetImportRegistry(registry *importctx.Registry) {
	if registry == nil {
		panic("plugin: import registry must not be nil")
	}
	a.importRegistryMu.Lock()
	defer a.importRegistryMu.Unlock()
	a.importRegistry = registry
}

// ImportRegistry returns the shared import row hook registry.
func (a *App) ImportRegistry() *importctx.Registry {
	a.importRegistryMu.Lock()
	defer a.importRegistryMu.Unlock()
	return a.importRegistry
}

// Import returns plugin-facing import row hook registration scoped to registrant.
func (a *App) Import(registrant string) *Import {
	a.importRegistryMu.Lock()
	defer a.importRegistryMu.Unlock()
	if a.importRegistry == nil {
		a.importRegistry = importctx.NewRegistry(a.Logger)
	}
	return &Import{registry: a.importRegistry, registrant: registrant}
}

func syncImportRowContext(dst *importctx.RowContext, src *extapi.ImportRowContext) {
	if dst == nil || src == nil {
		return
	}
	if len(src.Row) > 0 {
		if dst.Row == nil {
			dst.Row = make(map[string]string, len(src.Row))
		}
		for k, v := range src.Row {
			dst.Row[k] = v
		}
	}
	if len(src.Meta) > 0 {
		if dst.Meta == nil {
			dst.Meta = make(map[string]interface{}, len(src.Meta))
		}
		for k, v := range src.Meta {
			dst.Meta[k] = v
		}
	}
}

func toExtAPIImportRowContext(ctx *importctx.RowContext) *extapi.ImportRowContext {
	if ctx == nil {
		return &extapi.ImportRowContext{
			Row:  make(map[string]string),
			Meta: make(map[string]interface{}),
		}
	}
	out := &extapi.ImportRowContext{
		Entity:   ctx.Entity,
		RowIndex: ctx.RowIndex,
		Row:      make(map[string]string, len(ctx.Row)),
		Meta:     make(map[string]interface{}, len(ctx.Meta)),
	}
	for k, v := range ctx.Row {
		out.Row[k] = v
	}
	for k, v := range ctx.Meta {
		out.Meta[k] = v
	}
	return out
}
