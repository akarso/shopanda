package extension

import (
	"context"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

// ValueAPI is the request-facing port: HTTP, admin and GraphQL handlers depend on
// it. Every write method is gated by Access.PluginOnlyWrite. It deliberately omits
// the core-internal operations in CoreValueOps, so a handler cannot reach them.
type ValueAPI interface {
	Registry() *Registry
	List(ctx context.Context, target domainext.Target, includePrivate bool) ([]domainext.Value, error)
	ListByOrderItems(ctx context.Context, orderID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error)
	UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error)
	Delete(ctx context.Context, target domainext.Target, fieldCode string, canAccessPrivate bool) error
}

// CoreValueOps is the core-internal port: cart line removal, orphan cleanup,
// claim/merge copy and the order snapshot. These skip the PluginOnlyWrite guard on
// purpose (see ValueService). Only application services (cart, checkout) may depend
// on it — never handlers; each declares the subset it needs.
type CoreValueOps interface {
	DeleteAllForTarget(ctx context.Context, target domainext.Target) error
	DeleteOrphanCartItemExtensions(ctx context.Context, cartID string, keepVariantIDs []string) error
	CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error
	SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error
}
