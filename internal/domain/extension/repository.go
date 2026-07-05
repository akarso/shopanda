package extension

import "context"

// FieldRepository persists extension field definitions.
type FieldRepository interface {
	Save(ctx context.Context, field ExtensionField) error
	// FindByCode returns the active field for code.
	// When no active field exists, implementations must return apperror.NotFound
	// detectable via apperror.Is(err, apperror.CodeNotFound).
	FindByCode(ctx context.Context, code string) (ExtensionField, error)
	ListActive(ctx context.Context, scope TargetType) ([]ExtensionField, error)
	SoftDelete(ctx context.Context, code string) error
}
