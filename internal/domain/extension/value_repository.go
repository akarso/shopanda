package extension

import "context"

// ValueRepository persists extension field values.
type ValueRepository interface {
	ListByTarget(ctx context.Context, target Target) ([]Value, error)
	ListByTargets(ctx context.Context, targetType TargetType, targetIDs []string) ([]Value, error)
	Upsert(ctx context.Context, value Value) error
	UpsertBatch(ctx context.Context, values []Value) error
	Delete(ctx context.Context, target Target, fieldCode string) error
}
