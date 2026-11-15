package extension

import "context"

// PluginOnlyRetireStore soft-deletes plugin-only definitions and optionally purges values.
// FieldService soft-deletes and purges in separate calls so a restored active row is not
// soft-deleted again by a --purge-values re-run.
type PluginOnlyRetireStore interface {
	// RetireActive soft-deletes an active PluginOnlyWrite row and commits. When
	// purgeValues is true, then deletes extension_values in separate short
	// transactions (each batch locks the field row). Prefer purgeValues=false from
	// FieldService; use PurgeIfSoftDeletedPluginOnly for value deletes.
	RetireActive(ctx context.Context, code string, purgeValues bool) (valuesPurged int64, err error)
	// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
	// row exists for code. Timeout → ErrPurgeIncomplete; restored/missing definition
	// → ErrPurgeDefinitionChanged. Returns NotFound when no soft-deleted plugin-only
	// row exists (including never stored or active again).
	PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (valuesPurged int64, err error)
}
