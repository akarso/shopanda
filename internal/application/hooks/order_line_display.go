package hooks

import "github.com/akarso/shopanda/pkg/extapi"

// OrderLineDisplayFieldsFromContext returns fields collected during
// order.line.display (payload key "fields"). Re-reads after Invoke so a
// handler that allocated a new slice via Set is still visible.
func OrderLineDisplayFieldsFromContext(ctx *Context) []extapi.OrderLineDisplayField {
	if ctx == nil {
		return nil
	}
	raw, ok := ctx.Get("fields")
	if !ok || raw == nil {
		return nil
	}
	fields, ok := raw.(*[]extapi.OrderLineDisplayField)
	if !ok || fields == nil {
		return nil
	}
	return *fields
}

type orderLineFieldsSnapshot struct {
	present     bool
	originalPtr *[]extapi.OrderLineDisplayField
	copy        []extapi.OrderLineDisplayField
}

func takeOrderLineFieldsSnapshot(hookCtx *Context) orderLineFieldsSnapshot {
	if hookCtx == nil {
		return orderLineFieldsSnapshot{}
	}
	raw, ok := hookCtx.Get("fields")
	if !ok {
		return orderLineFieldsSnapshot{}
	}
	if raw == nil {
		return orderLineFieldsSnapshot{present: true}
	}
	fields, ok := raw.(*[]extapi.OrderLineDisplayField)
	if !ok || fields == nil {
		return orderLineFieldsSnapshot{present: true}
	}
	return orderLineFieldsSnapshot{
		present:     true,
		originalPtr: fields,
		copy:        append([]extapi.OrderLineDisplayField(nil), (*fields)...),
	}
}

// restoreOrderLineFieldsSnapshot puts the pre-handler fields back, including
// when the handler mutated elements in place or replaced the pointer.
func restoreOrderLineFieldsSnapshot(hookCtx *Context, snap orderLineFieldsSnapshot) {
	if hookCtx == nil {
		return
	}
	if hookCtx.Payload == nil {
		hookCtx.Payload = make(map[string]interface{})
	}
	if !snap.present {
		delete(hookCtx.Payload, "fields")
		return
	}
	restored := append([]extapi.OrderLineDisplayField(nil), snap.copy...)
	if snap.originalPtr != nil {
		*snap.originalPtr = restored
		hookCtx.Set("fields", snap.originalPtr)
		return
	}
	hookCtx.Set("fields", &restored)
}
