package extension

import "context"

// ValueRepository persists extension field values.
type ValueRepository interface {
	ListByTarget(ctx context.Context, target Target) ([]Value, error)
	ListByTargets(ctx context.Context, targetType TargetType, targetIDs []string) ([]Value, error)
	Upsert(ctx context.Context, value Value) error
	UpsertBatch(ctx context.Context, values []Value) error
	// UpsertBatchSelective upserts all values in one transaction. Codes listed
	// in exclusiveFieldCodes use the conditional ON CONFLICT WHERE predicate
	// (null/blank/equal only); others use a plain overwrite. Any exclusive
	// conflict rolls the whole batch back → ErrStringValueConflict.
	UpsertBatchSelective(ctx context.Context, values []Value, exclusiveFieldCodes map[string]struct{}) error
	Delete(ctx context.Context, target Target, fieldCode string) error
	// ListTargetIDsByTypeAndIDPrefix returns distinct target_ids for type where target_id has prefix (e.g. cartUUID+":").
	ListTargetIDsByTypeAndIDPrefix(ctx context.Context, targetType TargetType, idPrefix string) ([]string, error)
}
