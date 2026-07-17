package pluginsdk

import "github.com/akarso/shopanda/pkg/extapi"

// Import registers CSV import row hook handlers.
type Import struct {
	sdk *SDK
}

// Import returns import row hook registration helpers for the SDK plugin.
func (s *SDK) Import() *Import {
	return &Import{sdk: s}
}

// RegisterRow registers a handler for entity at priority (lower runs first).
func (i *Import) RegisterRow(entity extapi.ImportEntity, priority int, handler extapi.ImportRowHandler) error {
	return i.sdk.app.Import(i.sdk.plugin).RegisterRowHook(entity, priority, handler)
}

// RegisterProductRow registers a product import row hook.
func (i *Import) RegisterProductRow(priority int, handler extapi.ImportRowHandler) error {
	return i.RegisterRow(extapi.ImportEntityProduct, priority, handler)
}
