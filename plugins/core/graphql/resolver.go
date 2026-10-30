package graphql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// bundleLister lists bundle components (read-only).
type bundleLister interface {
	List(ctx context.Context, bundleID string) ([]catalog.BundleComponent, error)
}

// groupedLister lists grouped members (read-only).
type groupedLister interface {
	List(ctx context.Context, groupID string) ([]catalog.GroupedMember, error)
}

// productIDLister is the optional batch read on ProductRepository (postgres).
type productIDLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

// Resolver loads catalog data for GraphQL queries.
type Resolver struct {
	productRepo  catalog.ProductRepository
	categoryRepo catalog.CategoryRepository
	fields       *extensionapp.FieldService
	values       *extensionapp.ValueService

	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions

	bundles bundleLister
	grouped groupedLister
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

// WithVisibility enables PR-1056 axis gates (same helpers as REST PR-1057).
// Nil facts leaves the gate off (catalog.visibility.enforce false).
func (r *Resolver) WithVisibility(facts catalog.VisibilityFactsSource, opts catalog.VisibilityOptions) *Resolver {
	r.visibilityFacts = facts
	r.visibilityOpts = opts
	return r
}

// WithComposition wires read-only bundle / grouped list services.
func (r *Resolver) WithComposition(bundles bundleLister, grouped groupedLister) *Resolver {
	r.bundles = bundles
	r.grouped = grouped
	return r
}

func (r *Resolver) productByID(ctx context.Context, id string) (*catalog.Product, error) {
	if id == "" {
		return nil, fmt.Errorf("product id is required")
	}
	prod, err := r.productRepo.FindByID(ctx, id)
	if err != nil || prod == nil {
		return prod, err
	}
	ok, err := catalog.AllowWithFacts(ctx, prod, catalog.VisibilitySurfaceIndividually, r.visibilityFacts, r.visibilityOpts)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return prod, nil
}

func (r *Resolver) productBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if slug == "" {
		return nil, fmt.Errorf("product slug is required")
	}
	prod, err := r.productRepo.FindBySlug(ctx, slug)
	if err != nil || prod == nil {
		return prod, err
	}
	ok, err := catalog.AllowWithFacts(ctx, prod, catalog.VisibilitySurfaceIndividually, r.visibilityFacts, r.visibilityOpts)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return prod, nil
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

func listVisibleParams(offset, limit int, after string) (catalog.ListVisiblePageParams, error) {
	limit = normalizeLimit(limit)
	if after != "" {
		o, s, err := catalog.DecodeListCursor(after)
		if err != nil {
			// Match REST parseVisibilityAfter: Validation so GraphQL returns HTTP 400.
			return catalog.ListVisiblePageParams{}, apperror.Validation(err.Error())
		}
		return catalog.ListVisiblePageParams{
			AfterSet:    true,
			AfterOffset: o,
			AfterSkip:   s,
			Limit:       limit,
		}, nil
	}
	if offset < 0 {
		// Match REST ParsePagination text.
		return catalog.ListVisiblePageParams{}, apperror.Validation("offset must be a non-negative integer")
	}
	return catalog.ListVisiblePageParams{VisibleSkip: offset, Limit: limit}, nil
}

func visiblePageToConnection(page catalog.VisiblePage) productConnectionGraph {
	conn := productConnectionGraph{
		Items:        productsToGraph(page.Products),
		Truncated:    page.Truncated,
		HasMore:      page.HasMore,
		DepthLimited: page.DepthLimited,
	}
	if page.NextAfter != "" {
		s := page.NextAfter
		conn.NextAfter = &s
	}
	return conn
}

func (r *Resolver) products(ctx context.Context, offset, limit int, after string) (productConnectionGraph, error) {
	params, err := listVisibleParams(offset, limit, after)
	if err != nil {
		return productConnectionGraph{}, err
	}
	page, err := catalog.ListVisiblePage(
		ctx,
		func(ctx context.Context, dbOffset, dbLimit int) ([]catalog.Product, error) {
			return r.productRepo.List(ctx, catalog.ListFilter{Offset: dbOffset, Limit: dbLimit})
		},
		catalog.VisibilitySurfaceCatalog,
		r.visibilityFacts,
		r.visibilityOpts,
		params,
	)
	if err != nil {
		return productConnectionGraph{}, err
	}
	return visiblePageToConnection(page), nil
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

func (r *Resolver) categoryProducts(ctx context.Context, categoryID string, offset, limit int, after string) (productConnectionGraph, error) {
	if categoryID == "" {
		return productConnectionGraph{}, fmt.Errorf("category id is required")
	}
	params, err := listVisibleParams(offset, limit, after)
	if err != nil {
		return productConnectionGraph{}, err
	}
	page, err := catalog.ListVisiblePage(
		ctx,
		func(ctx context.Context, dbOffset, dbLimit int) ([]catalog.Product, error) {
			return r.productRepo.FindByCategoryID(ctx, categoryID, dbOffset, dbLimit)
		},
		catalog.VisibilitySurfaceCatalog,
		r.visibilityFacts,
		r.visibilityOpts,
		params,
	)
	if err != nil {
		return productConnectionGraph{}, err
	}
	return visiblePageToConnection(page), nil
}

func (r *Resolver) productsByID(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	out := map[string]*catalog.Product{}
	if len(ids) == 0 {
		return out, nil
	}
	if lister, ok := r.productRepo.(productIDLister); ok {
		byID, err := lister.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("list children: %w", err)
		}
		if byID == nil {
			return out, nil
		}
		return byID, nil
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		child, err := r.productRepo.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load child %s: %w", id, err)
		}
		if child != nil {
			out[id] = child
		}
	}
	return out, nil
}

func (r *Resolver) bundleComponents(ctx context.Context, pg productGraph) ([]bundleComponentGraph, error) {
	if pg.Type != string(catalog.TypeBundle) || r.bundles == nil {
		return []bundleComponentGraph{}, nil
	}
	rows, err := r.bundles.List(ctx, pg.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ComponentProductID)
	}
	byID, err := r.productsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	allowed, err := r.filterChildrenIndividually(ctx, byID)
	if err != nil {
		return nil, err
	}
	out := make([]bundleComponentGraph, 0, len(rows))
	for _, row := range rows {
		child, ok := allowed[row.ComponentProductID]
		if !ok {
			continue
		}
		out = append(out, bundleComponentGraph{
			ComponentProductID: row.ComponentProductID,
			Quantity:           row.Quantity,
			Required:           row.Required,
			Name:               child.Name,
			Slug:               child.Slug,
			Type:               string(child.Type),
			CreatedAt:          formatTime(row.CreatedAt),
		})
	}
	return out, nil
}

func (r *Resolver) groupedMembers(ctx context.Context, pg productGraph) ([]groupedMemberGraph, error) {
	if pg.Type != string(catalog.TypeGrouped) || r.grouped == nil {
		return []groupedMemberGraph{}, nil
	}
	rows, err := r.grouped.List(ctx, pg.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.MemberProductID)
	}
	byID, err := r.productsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	allowed, err := r.filterChildrenIndividually(ctx, byID)
	if err != nil {
		return nil, err
	}
	out := make([]groupedMemberGraph, 0, len(rows))
	for _, row := range rows {
		child, ok := allowed[row.MemberProductID]
		if !ok {
			continue
		}
		out = append(out, groupedMemberGraph{
			MemberProductID: row.MemberProductID,
			Name:            child.Name,
			Slug:            child.Slug,
			Type:            string(child.Type),
			CreatedAt:       formatTime(row.CreatedAt),
		})
	}
	return out, nil
}

// filterChildrenIndividually batches Individually-surface visibility via
// FilterWithFacts. Missing IDs (absent from byID) and denied rows are omitted;
// LoadMany/FilterWithFacts errors fail closed.
func (r *Resolver) filterChildrenIndividually(ctx context.Context, byID map[string]*catalog.Product) (map[string]catalog.Product, error) {
	out := map[string]catalog.Product{}
	if len(byID) == 0 {
		return out, nil
	}
	products := make([]catalog.Product, 0, len(byID))
	for _, p := range byID {
		if p == nil {
			continue
		}
		products = append(products, *p)
	}
	if len(products) == 0 {
		return out, nil
	}
	filtered, err := catalog.FilterWithFacts(ctx, products, catalog.VisibilitySurfaceIndividually, r.visibilityFacts, r.visibilityOpts)
	if err != nil {
		return nil, err
	}
	for _, p := range filtered {
		out[p.ID] = p
	}
	return out, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func productFromSource(source interface{}) (productGraph, bool) {
	switch v := source.(type) {
	case productGraph:
		return v, true
	case *productGraph:
		if v == nil {
			return productGraph{}, false
		}
		return *v, true
	default:
		return productGraph{}, false
	}
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

func optionalStringArg(args map[string]interface{}, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return strings.TrimSpace(s), nil
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
