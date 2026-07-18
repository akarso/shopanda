package plugin

import (
	"fmt"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Export exposes export row hook registration to plugins during Init.
type Export struct {
	registry   *exportctx.Registry
	registrant string
}

// RegisterRowHook adds a handler for entity at priority (lower runs first).
func (e *Export) RegisterRowHook(entity extapi.ExportEntity, priority int, handler extapi.ExportRowHandler) error {
	if e == nil || e.registry == nil {
		return fmt.Errorf("plugin: export registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: export row handler must not be nil")
	}
	return e.registry.Register(string(entity), priority, e.registrant, func(ctx *exportctx.RowContext) error {
		extCtx := toExtAPIExportRowContext(ctx)
		if err := handler(extCtx); err != nil {
			return err
		}
		syncExportRowContext(ctx, extCtx)
		return nil
	})
}

// SetExportRegistry wires the shared export row hook registry before plugin Init.
func (a *App) SetExportRegistry(registry *exportctx.Registry) {
	if registry == nil {
		panic("plugin: export registry must not be nil")
	}
	a.exportRegistryMu.Lock()
	defer a.exportRegistryMu.Unlock()
	a.exportRegistry = registry
}

// ExportRegistry returns the shared export row hook registry.
func (a *App) ExportRegistry() *exportctx.Registry {
	a.exportRegistryMu.Lock()
	defer a.exportRegistryMu.Unlock()
	return a.exportRegistry
}

// Export returns plugin-facing export row hook registration scoped to registrant.
func (a *App) Export(registrant string) *Export {
	a.exportRegistryMu.Lock()
	defer a.exportRegistryMu.Unlock()
	if a.exportRegistry == nil {
		a.exportRegistry = exportctx.NewRegistry(a.Logger)
	}
	return &Export{registry: a.exportRegistry, registrant: registrant}
}

func syncExportRowContext(dst *exportctx.RowContext, src *extapi.ExportRowContext) {
	if dst == nil || src == nil {
		return
	}
	dst.Skip = src.Skip
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
	if len(src.Errors) > 0 {
		dst.Errors = append(dst.Errors, extExportErrors(src.Errors)...)
	}
}

func extExportErrors(src []extapi.ExportError) []exportctx.ExportError {
	out := make([]exportctx.ExportError, len(src))
	for i, e := range src {
		out[i] = exportctx.ExportError{
			RowIndex: e.RowIndex,
			Code:     e.Code,
			Message:  e.Message,
		}
	}
	return out
}

func toExtAPIExportRowContext(ctx *exportctx.RowContext) *extapi.ExportRowContext {
	if ctx == nil {
		return &extapi.ExportRowContext{
			Row:  make(map[string]string),
			Meta: make(map[string]interface{}),
		}
	}
	out := &extapi.ExportRowContext{
		Entity:   ctx.Entity,
		RowIndex: ctx.RowIndex,
		Skip:     ctx.Skip,
		Row:      make(map[string]string, len(ctx.Row)),
		Meta:     make(map[string]interface{}, len(ctx.Meta)),
	}
	for k, v := range ctx.Row {
		out.Row[k] = v
	}
	for k, v := range ctx.Meta {
		out.Meta[k] = v
	}
	if len(ctx.Errors) > 0 {
		out.Errors = make([]extapi.ExportError, len(ctx.Errors))
		for i, e := range ctx.Errors {
			out.Errors[i] = extapi.ExportError{
				RowIndex: e.RowIndex,
				Code:     e.Code,
				Message:  e.Message,
			}
		}
	}
	return out
}
