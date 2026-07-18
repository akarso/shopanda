package pluginsdk

import "github.com/akarso/shopanda/pkg/extapi"

// Export registers CSV export row hook handlers.
type Export struct {
	sdk *SDK
}

// Export returns export row hook registration helpers for the SDK plugin.
func (s *SDK) Export() *Export {
	return &Export{sdk: s}
}

// RegisterRow registers a handler for entity at priority (lower runs first).
func (e *Export) RegisterRow(entity extapi.ExportEntity, priority int, handler extapi.ExportRowHandler) error {
	return e.sdk.app.Export(e.sdk.plugin).RegisterRowHook(entity, priority, handler)
}

// RegisterProductRow registers a product export row hook.
func (e *Export) RegisterProductRow(priority int, handler extapi.ExportRowHandler) error {
	return e.RegisterRow(extapi.ExportEntityProduct, priority, handler)
}
