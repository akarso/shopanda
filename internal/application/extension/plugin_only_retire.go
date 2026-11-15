package extension

import "context"

// PluginOnlyRetireStore soft-deletes plugin-only definitions and optionally
// purges their values. Implementations that purge must do so in the same
// transaction as the soft-delete so a purge failure leaves the definition active.
type PluginOnlyRetireStore interface {
	// RetireActive soft-deletes an active PluginOnlyWrite row. When purgeValues
	// is true, deletes extension_values for the code in the same transaction.
	RetireActive(ctx context.Context, code string, purgeValues bool) (valuesPurged int64, err error)
	// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
	// row exists for code (retry after a failed purge). Returns NotFound when no
	// such row exists (including never stored).
	PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (valuesPurged int64, err error)
}
