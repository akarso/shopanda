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

// SetBus enables publishing catalog.EventProductUpdated and
// catalog.EventCategoryUpdated on every successful Assign/Unassign
// (PR-1036: the search index's on-save subscriber listens for the product
// event, refreshing the product's indexed CategoryIDs; reusing it rather
// than adding a new one also means the existing cache invalidation
// subscriber now correctly invalidates on a category assignment change
// too, not just a product field edit). PR-1037 added the category event:
// without it, a category's indexed ProductCount only ever gets refreshed
// on that category's own next create/update, never on a membership change
// via this handler — see publishAssignmentChanged. The full product
// (Name/Slug/Status) and category (Name/Slug), not just their IDs, must
// be populated on the published events: rewrite.Subscriber also listens
// to both synchronously (bus.On, not OnAsync) and builds a URL rewrite
// from data.Slug directly, with no fallback lookup — an empty Slug here
// would register "/" itself as the rewrite (or fail with a path conflict
// if "/" is already claimed by something else). Either failure mode would
// normally also abort dispatch to the async cache-invalidation/
// search-reindex handlers (a failed sync handler aborts the whole Publish
// call) — publishAssignmentChanged's own PublishAsync fallback exists
// precisely so those don't go stale over a rewrite-specific problem. Left
// unset (nil), Assign/Unassign work exactly as before.
func (h *CategoryProductAssignmentAdminHandler) SetBus(bus *event.Bus) {
	h.bus = bus
}

// publishAssignmentChanged publishes both the product-side and
// category-side events for an assignment change and returns the first
// error (if any) rather than discarding it: a synchronous handler on
// either event (rewrite.Subscriber) can genuinely fail even with an
// accurate payload — a real path conflict with an existing rewrite, or a
// repository error — and per event.Bus's own contract, a failed sync
// handler aborts the whole Publish call, so the async cache-invalidation/
// search-reindex handlers would never run either. That coupling is
// exactly what's undesirable here: this assignment already succeeded, and
// an unrelated rewrite failure must not also leave the search index/cache
// stale. So each Publish that fails falls back to PublishAsync,
// dispatching that event straight to its async handlers regardless of
// why the sync phase failed — the index/cache side effects still happen
// even though the rewrite one didn't. The caller doesn't fail the request
// over any of this either way (the assignment write itself already
// succeeded, and this codebase's own convention — see product_admin.go's
// own Publish call — never blocks an admin response on a side-effect's
// own success), but does audit a failure separately, so it's visible to
// whoever reviews the audit log instead of silently invisible.
func (h *CategoryProductAssignmentAdminHandler) publishAssignmentChanged(ctx context.Context, product *catalog.Product, category *catalog.Category) error {
	if h.bus == nil {
		return nil
	}
	productEvt := event.New(catalog.EventProductUpdated, "category.assignment", catalog.ProductUpdatedData{
		ProductID: product.ID,
		Name:      product.Name,
		Slug:      product.Slug,
		Status:    product.Status,
	})
	err := h.bus.Publish(ctx, productEvt)
	if err != nil {
		h.bus.PublishAsync(productEvt)
	}

	// Reuses EventCategoryUpdated (as the product side reuses
	// EventProductUpdated) with the category's own current Name/Slug —
	// unchanged by this handler, so OldSlug is left empty and
	// rewrite.Subscriber's HandleCategoryUpdated re-saves the existing
	// rewrite (a harmless idempotent write), while
	// search.IndexUpdateSubscriber.HandleCategoryUpdated re-reads the
	// category's current ProductCount and re-indexes it.
	categoryEvt := event.New(catalog.EventCategoryUpdated, "category.assignment", catalog.CategoryUpdatedData{
		CategoryID: category.ID,
		Name:       category.Name,
		Slug:       category.Slug,
	})
	if catErr := h.bus.Publish(ctx, categoryEvt); catErr != nil {
		h.bus.PublishAsync(categoryEvt)
		if err == nil {
			err = catErr
		}
	}
	return err
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

// validateAssignmentTargets also returns the fetched category and product
// (not just the category's ID): Assign/Unassign need the product's
// Name/Slug/Status and the category's Name/Slug to publish accurate
// catalog.EventProductUpdated/EventCategoryUpdated events afterward — see
// publishAssignmentChanged.
func (h *CategoryProductAssignmentAdminHandler) validateAssignmentTargets(r *http.Request) (*catalog.Category, *catalog.Product, error) {
	categoryID := r.PathValue("id")
	if categoryID == "" {
		return nil, nil, apperror.Validation("category id is required")
	}
	productID := r.PathValue("productId")
	if productID == "" {
		return nil, nil, apperror.Validation("product id is required")
	}
	category, err := h.categories.FindByID(r.Context(), categoryID)
	if err != nil {
		return nil, nil, err
	}
	if category == nil {
		return nil, nil, apperror.NotFound("category not found")
	}
	product, err := h.products.FindByID(r.Context(), productID)
	if err != nil {
		return nil, nil, err
	}
	if product == nil {
		return nil, nil, apperror.NotFound("product not found")
	}
	return category, product, nil
}

// Assign handles POST /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Assign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category, product, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		categoryID, productID := category.ID, product.ID
		if err := h.assignments.AssignCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, nil)
		if pubErr := h.publishAssignmentChanged(r.Context(), product, category); pubErr != nil {
			h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID,
				fmt.Errorf("category assigned, and search index/cache were still updated, but a synchronous event handler (e.g. URL rewrite) failed: %w", pubErr))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": true})
	}
}

// Unassign handles DELETE /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Unassign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category, product, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		categoryID, productID := category.ID, product.ID
		if err := h.assignments.RemoveCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, nil)
		if pubErr := h.publishAssignmentChanged(r.Context(), product, category); pubErr != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID,
				fmt.Errorf("category unassigned, and search index/cache were still updated, but a synchronous event handler (e.g. URL rewrite) failed: %w", pubErr))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": false})
	}
}
