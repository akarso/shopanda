package graphql

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// Resolver loads catalog data for GraphQL queries.
type Resolver struct {
	productRepo  catalog.ProductRepository
	categoryRepo catalog.CategoryRepository
	fields       *extensionapp.FieldService
	values       *extensionapp.ValueService
}

// NewResolver creates a Resolver.
func NewResolver(products catalog.ProductRepository, categories catalog.CategoryRepository) (*Resolver, error) {
	if products == nil {
		return nil, fmt.Errorf("graphql resolver: products repository must not be nil")
	}
	if categories == nil {
		return nil, fmt.Errorf("graphql resolver: categories repository must not be nil")
	}
	return &Resolver{productRepo: products, categoryRepo: categories}, nil
}

// WithExtensions wires extension services into the GraphQL resolver.
func (r *Resolver) WithExtensions(fields *extensionapp.FieldService, values *extensionapp.ValueService) *Resolver {
	r.fields = fields
	r.values = values
	return r
}

func (r *Resolver) productByID(ctx context.Context, id string) (*catalog.Product, error) {
	if id == "" {
		return nil, fmt.Errorf("product id is required")
	}
	return r.productRepo.FindByID(ctx, id)
}

func (r *Resolver) productBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if slug == "" {
		return nil, fmt.Errorf("product slug is required")
	}
	return r.productRepo.FindBySlug(ctx, slug)
}

// normalizeLimit applies the shared pagination bounds: non-positive limits fall
// back to defaultListLimit and limits above maxListLimit are capped.
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func (r *Resolver) products(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must be >= 0")
	}
	return r.productRepo.List(ctx, offset, normalizeLimit(limit))
}

func (r *Resolver) categoryByID(ctx context.Context, id string) (*catalog.Category, error) {
	if id == "" {
		return nil, fmt.Errorf("category id is required")
	}
	return r.categoryRepo.FindByID(ctx, id)
}

func (r *Resolver) categories(ctx context.Context) ([]catalog.Category, error) {
	return r.categoryRepo.FindAll(ctx)
}

func (r *Resolver) categoryProducts(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if categoryID == "" {
		return nil, fmt.Errorf("category id is required")
	}
	if offset < 0 {
		return nil, fmt.Errorf("offset must be >= 0")
	}
	return r.productRepo.FindByCategoryID(ctx, categoryID, offset, normalizeLimit(limit))
}

func intArg(args map[string]interface{}, name string, fallback int) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}

func stringArg(args map[string]interface{}, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", fmt.Errorf("%s is required", name)
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return s, nil
}

func boolArg(args map[string]interface{}, name string, fallback bool) (bool, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback, nil
	}
	v, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return v, nil
}

func (r *Resolver) requirePermission(ctx context.Context, perm rbac.Permission) error {
	ac, err := admin.FromContext(ctx)
	if err != nil || ac == nil || !ac.HasPermission(string(perm)) {
		return apperror.Forbidden("insufficient permissions")
	}
	return nil
}

func (r *Resolver) canReadPrivate(ctx context.Context) bool {
	ac, err := admin.FromContext(ctx)
	if err != nil || ac == nil {
		return false
	}
	return ac.HasPermission(string(rbac.ExtensionsPrivateRead))
}

func (r *Resolver) updatedBy(ctx context.Context) string {
	ac, err := admin.FromContext(ctx)
	if err != nil || ac == nil {
		return ""
	}
	return strings.TrimSpace(ac.AdminID)
}

func (r *Resolver) extensionFields(ctx context.Context, scope string, includePrivate bool) ([]domainext.ExtensionField, error) {
	if err := r.requirePermission(ctx, rbac.ExtensionsRead); err != nil {
		return nil, err
	}
	if r.fields == nil {
		return nil, apperror.Internal("extension field service unavailable")
	}
	if includePrivate && !r.canReadPrivate(ctx) {
		includePrivate = false
	}
	return r.fields.List(extensionapp.ListFilter{
		Scope:          domainext.TargetType(strings.TrimSpace(scope)),
		IncludePrivate: includePrivate,
	}), nil
}

func (r *Resolver) extensionValues(ctx context.Context, targetType, targetID string, includePrivate bool) ([]domainext.Value, error) {
	if err := r.requirePermission(ctx, rbac.ExtensionsRead); err != nil {
		return nil, err
	}
	if r.values == nil {
		return nil, apperror.Internal("extension value service unavailable")
	}
	if includePrivate && !r.canReadPrivate(ctx) {
		includePrivate = false
	}
	values, err := r.values.List(ctx, domainext.Target{
		Type: domainext.TargetType(strings.TrimSpace(targetType)),
		ID:   strings.TrimSpace(targetID),
	}, includePrivate)
	if err != nil {
		return nil, extensionAPIError(err)
	}
	return values, nil
}

func (r *Resolver) upsertExtensionValues(ctx context.Context, targetType, targetID string, inputs []domainext.ValueInput) ([]domainext.Value, error) {
	if err := r.requirePermission(ctx, rbac.ExtensionsWrite); err != nil {
		return nil, err
	}
	if r.values == nil {
		return nil, apperror.Internal("extension value service unavailable")
	}
	out, err := r.values.UpsertBatch(ctx, domainext.Target{
		Type: domainext.TargetType(strings.TrimSpace(targetType)),
		ID:   strings.TrimSpace(targetID),
	}, inputs, r.updatedBy(ctx), r.canReadPrivate(ctx))
	if err != nil {
		return nil, extensionAPIError(err)
	}
	return out, nil
}

func (r *Resolver) deleteExtensionValue(ctx context.Context, targetType, targetID, fieldCode string) (bool, error) {
	if err := r.requirePermission(ctx, rbac.ExtensionsWrite); err != nil {
		return false, err
	}
	if r.values == nil {
		return false, apperror.Internal("extension value service unavailable")
	}
	err := r.values.Delete(ctx, domainext.Target{
		Type: domainext.TargetType(strings.TrimSpace(targetType)),
		ID:   strings.TrimSpace(targetID),
	}, strings.TrimSpace(fieldCode), r.canReadPrivate(ctx))
	if err != nil {
		return false, extensionAPIError(err)
	}
	return true, nil
}

func extensionAPIError(err error) error {
	if err == nil {
		return nil
	}
	if domainext.IsValidationError(err) {
		return apperror.FieldValidationFailed(err.Error())
	}
	if errors.Is(err, domainext.ErrUnknownFieldCode) {
		return apperror.UnknownFieldCode(err.Error())
	}
	if errors.Is(err, domainext.ErrForbiddenPrivateField) {
		return apperror.ForbiddenPrivateField(err.Error())
	}
	return err
}
