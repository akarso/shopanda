package extension

import "context"

// FieldRepository persists extension field definitions.
type FieldRepository interface {
	Save(ctx context.Context, field ExtensionField) error
	FindByCode(ctx context.Context, code string) (ExtensionField, error)
	ListActive(ctx context.Context, scope TargetType) ([]ExtensionField, error)
	SoftDelete(ctx context.Context, code string) error
}
