package extension

import (
	"context"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// ValueAPI is the request-facing port: HTTP, admin and GraphQL handlers depend on
// it. Every write method is gated by Access.PluginOnlyWrite. It deliberately omits
// the core-internal operations listed at the bottom of this file.
type ValueAPI interface {
	Registry() *Registry
	List(ctx context.Context, target domainext.Target, includePrivate bool) ([]domainext.Value, error)
	ListByOrderItems(ctx context.Context, orderID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error)
	UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error)
	Delete(ctx context.Context, target domainext.Target, fieldCode string, canAccessPrivate bool) error
}

// AsValueAPI converts v to ValueAPI, returning an untyped nil interface for a nil
// pointer. Use it at wiring boundaries: a nil *ValueService stored directly in a
// ValueAPI is a non-nil interface and would bypass handlers' `== nil` checks.
func AsValueAPI(v *ValueService) ValueAPI {
	if v == nil {
		return nil
	}
	return v
}

var _ ValueAPI = (*ValueService)(nil)

// The core-internal operations (DeleteAllForTarget, DeleteOrphanCartItemExtensions,
// CopyTarget, SnapshotCartItemToOrderItem) are intentionally not part of ValueAPI.
// Cart and checkout services declare the subset they need as local interfaces.
