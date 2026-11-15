package extension

import "context"

// PluginOnlyRetireStore soft-deletes plugin-only definitions and optionally purges values.
// Soft-delete commits before value deletes so a purge timeout keeps the definition retired
// and completed batches; remaining rows are removed by PurgeIfSoftDeletedPluginOnly.
type PluginOnlyRetireStore interface {
	// RetireActive soft-deletes an active PluginOnlyWrite row and commits. When
	// purgeValues is true, then deletes extension_values in separate short
	// transactions. A purge failure returns domainext.ErrPurgeIncomplete after
	// the soft-delete has committed.
	RetireActive(ctx context.Context, code string, purgeValues bool) (valuesPurged int64, err error)
	// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
	// row exists for code (retry after an incomplete purge). Returns NotFound when
	// no such row exists (including never stored).
	PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (valuesPurged int64, err error)
}
