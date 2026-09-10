package admin

import (
	"context"
	"fmt"
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// CategoryProductAssignmentAdminHandler manages product-category assignment writes.
type CategoryProductAssignmentAdminHandler struct {
	categories  catalog.CategoryRepository
	products    catalog.ProductRepository
	assignments catalog.ProductCategoryAssignmentRepository
	auditor     *admin.Auditor
	bus         *event.Bus // optional; nil (the default) skips publishing — see SetBus.
}

// NewCategoryProductAssignmentAdminHandler creates a CategoryProductAssignmentAdminHandler with a default auditor.
func NewCategoryProductAssignmentAdminHandler(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
	assignments catalog.ProductCategoryAssignmentRepository,
) *CategoryProductAssignmentAdminHandler {
	return NewCategoryProductAssignmentAdminHandlerWithAuditor(categories, products, assignments, admin.NewAuditor(logger.New("info")))
}

// NewCategoryProductAssignmentAdminHandlerWithAuditor creates a CategoryProductAssignmentAdminHandler with a custom auditor.
func NewCategoryProductAssignmentAdminHandlerWithAuditor(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
	assignments catalog.ProductCategoryAssignmentRepository,
	auditor *admin.Auditor,
) *CategoryProductAssignmentAdminHandler {
	if categories == nil {
		panic("CategoryProductAssignmentAdminHandler: categories repository must not be nil")
	}
	if products == nil {
		panic("CategoryProductAssignmentAdminHandler: products repository must not be nil")
	}
	if assignments == nil {
		panic("CategoryProductAssignmentAdminHandler: assignments repository must not be nil")
	}
	if auditor == nil {
		panic("CategoryProductAssignmentAdminHandler: auditor must not be nil")
	}
	return &CategoryProductAssignmentAdminHandler{
		categories:  categories,
		products:    products,
		assignments: assignments,
		auditor:     auditor,
	}
}

// SetBus enables publishing catalog.EventProductUpdated on every successful
// Assign/Unassign (PR-1036: the search index's on-save subscriber listens
// for it, refreshing the product's indexed category membership; reusing
// this event rather than adding a new one also means the existing cache
// invalidation subscriber now correctly invalidates on a category
// assignment change too, not just a product field edit). The full product
// (Name/Slug/Status), not just its ID, must be populated on the published
// event: rewrite.Subscriber also listens to this event synchronously
// (bus.On, not OnAsync) and builds a URL rewrite from data.Slug directly,
// with no fallback lookup — an empty Slug here would register "/" itself
// as this product's rewrite (or fail with a path conflict if "/" is
// already claimed by something else), and either way would abort the
// publish before the async cache-invalidation/search-reindex handlers
// even ran (sync handler errors short-circuit Publish). Left unset (nil),
// Assign/Unassign work exactly as before.
func (h *CategoryProductAssignmentAdminHandler) SetBus(bus *event.Bus) {
	h.bus = bus
}

// publishAssignmentChanged returns Publish's error (if any) rather than
// discarding it: a synchronous handler on this event (rewrite.Subscriber)
// can genuinely fail even with an accurate payload — a real path
// conflict with an existing rewrite, or a repository error — and per
// event.Bus's own contract, a failed sync handler aborts the whole
// Publish call, so the async cache-invalidation/search-reindex handlers
// never run either. The caller doesn't fail the request over this (the
// assignment write itself already succeeded, and this codebase's own
// convention — see product_admin.go's own Publish call — never blocks an
// admin response on a side-effect's own success) but does audit it
// separately, so a stale index/cache from this is visible to whoever
// reviews the audit log instead of silently invisible.
func (h *CategoryProductAssignmentAdminHandler) publishAssignmentChanged(ctx context.Context, product *catalog.Product) error {
	if h.bus == nil {
		return nil
	}
	return h.bus.Publish(ctx, event.New(catalog.EventProductUpdated, "category.assignment", catalog.ProductUpdatedData{
		ProductID: product.ID,
		Name:      product.Name,
		Slug:      product.Slug,
		Status:    product.Status,
	}))
}

func (h *CategoryProductAssignmentAdminHandler) audit(r *http.Request, action admin.AuditAction, categoryID, productID string, err error) {
	details := mergeAuditDetails(map[string]interface{}{"product_id": productID}, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "category",
		ResourceID:   categoryID,
		Details:      details,
		Result:       result,
		Error:        errMsg,
	})
}

// validateAssignmentTargets also returns the fetched product (not just its
// ID): Assign/Unassign need its Name/Slug/Status to publish an accurate
// catalog.EventProductUpdated afterward — see publishAssignmentChanged.
func (h *CategoryProductAssignmentAdminHandler) validateAssignmentTargets(r *http.Request) (string, *catalog.Product, error) {
	categoryID := r.PathValue("id")
	if categoryID == "" {
		return "", nil, apperror.Validation("category id is required")
	}
	productID := r.PathValue("productId")
	if productID == "" {
		return "", nil, apperror.Validation("product id is required")
	}
	category, err := h.categories.FindByID(r.Context(), categoryID)
	if err != nil {
		return "", nil, err
	}
	if category == nil {
		return "", nil, apperror.NotFound("category not found")
	}
	product, err := h.products.FindByID(r.Context(), productID)
	if err != nil {
		return "", nil, err
	}
	if product == nil {
		return "", nil, apperror.NotFound("product not found")
	}
	return categoryID, product, nil
}

// Assign handles POST /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Assign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID, product, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		productID := product.ID
		if err := h.assignments.AssignCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, nil)
		if pubErr := h.publishAssignmentChanged(r.Context(), product); pubErr != nil {
			h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID,
				fmt.Errorf("category assigned, but event publish failed — search index/cache may be stale until the next update or manual reindex: %w", pubErr))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": true})
	}
}

// Unassign handles DELETE /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Unassign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID, product, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		productID := product.ID
		if err := h.assignments.RemoveCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, nil)
		if pubErr := h.publishAssignmentChanged(r.Context(), product); pubErr != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID,
				fmt.Errorf("category unassigned, but event publish failed — search index/cache may be stale until the next update or manual reindex: %w", pubErr))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": false})
	}
}
