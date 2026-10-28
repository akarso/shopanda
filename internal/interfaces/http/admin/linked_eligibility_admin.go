package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// LinkedEligibilityAdminHandler lists and edits linked-product eligibility
// rules through LinkedEligibilityService (PR-1073). Eligibility is not
// composition and is not instance assignment.
type LinkedEligibilityAdminHandler struct {
	products   catalog.ProductRepository
	categories catalog.CategoryRepository
	rules      *catalogApp.LinkedEligibilityService
	auditor    *admin.Auditor
}

// NewLinkedEligibilityAdminHandler creates a handler over the eligibility service.
// products and categories enrich list rows; writes go through the service.
func NewLinkedEligibilityAdminHandler(
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	rules *catalogApp.LinkedEligibilityService,
	auditor *admin.Auditor,
) *LinkedEligibilityAdminHandler {
	if products == nil {
		panic("http: linked eligibility handler: product repository must not be nil")
	}
	if categories == nil {
		panic("http: linked eligibility handler: category repository must not be nil")
	}
	if rules == nil {
		panic("http: linked eligibility handler: eligibility service must not be nil")
	}
	if auditor == nil {
		panic("http: linked eligibility handler: auditor must not be nil")
	}
	return &LinkedEligibilityAdminHandler{
		products:   products,
		categories: categories,
		rules:      rules,
		auditor:    auditor,
	}
}

type linkedEligibleParentDTO struct {
	ParentProductID string    `json:"parent_product_id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Type            string    `json:"type"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

type linkedEligibleCategoryDTO struct {
	CategoryID string    `json:"category_id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

type addLinkedEligibleParentRequest struct {
	ParentProductID string `json:"parent_product_id"`
}

type addLinkedEligibleCategoryRequest struct {
	CategoryID string `json:"category_id"`
}

// ListParents handles GET /api/v1/admin/products/{id}/linked-eligible-parents.
func (h *LinkedEligibilityAdminHandler) ListParents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.rules.ListParents(r.Context(), childID)
		if err != nil {
			h.audit(r, admin.AuditLinkedEligibleParentList, childID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		parents, err := h.productsByID(r.Context(), parentProductIDs(rows))
		if err != nil {
			h.audit(r, admin.AuditLinkedEligibleParentList, childID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		out := make([]linkedEligibleParentDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, linkedParentDTOFrom(row, parents[row.ParentProductID]))
		}
		h.audit(r, admin.AuditLinkedEligibleParentList, childID, map[string]interface{}{"count": len(out)}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"parents": out})
	}
}

// AddParent handles POST /api/v1/admin/products/{id}/linked-eligible-parents.
// A duplicate pair is insert-ignore success.
func (h *LinkedEligibilityAdminHandler) AddParent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		var req addLinkedEligibleParentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.audit(r, admin.AuditLinkedEligibleParentAdd, childID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		link := &catalog.LinkedEligibleParent{
			ChildProductID:  childID,
			ParentProductID: req.ParentProductID,
		}
		if err := h.rules.AddParent(r.Context(), link); err != nil {
			h.audit(r, admin.AuditLinkedEligibleParentAdd, childID, map[string]interface{}{
				"parent_product_id": strings.TrimSpace(req.ParentProductID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		parents, _ := h.productsByID(r.Context(), []string{link.ParentProductID})
		dto := linkedParentDTOFrom(*link, parents[link.ParentProductID])
		h.audit(r, admin.AuditLinkedEligibleParentAdd, childID, map[string]interface{}{
			"parent_product_id": dto.ParentProductID,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"parent": dto})
	}
}

// RemoveParent handles DELETE /api/v1/admin/products/{id}/linked-eligible-parents/{parentId}.
func (h *LinkedEligibilityAdminHandler) RemoveParent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		parentID := strings.TrimSpace(r.PathValue("parentId"))
		details := map[string]interface{}{"parent_product_id": parentID}
		if err := h.rules.RemoveParent(r.Context(), childID, parentID); err != nil {
			h.audit(r, admin.AuditLinkedEligibleParentRemove, childID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditLinkedEligibleParentRemove, childID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

// ListCategories handles GET /api/v1/admin/products/{id}/linked-eligible-categories.
func (h *LinkedEligibilityAdminHandler) ListCategories() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.rules.ListCategories(r.Context(), childID)
		if err != nil {
			h.audit(r, admin.AuditLinkedEligibleCategoryList, childID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		out := make([]linkedEligibleCategoryDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, h.categoryDTOFrom(r.Context(), row))
		}
		h.audit(r, admin.AuditLinkedEligibleCategoryList, childID, map[string]interface{}{"count": len(out)}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"categories": out})
	}
}

// AddCategory handles POST /api/v1/admin/products/{id}/linked-eligible-categories.
// A duplicate pair is insert-ignore success.
func (h *LinkedEligibilityAdminHandler) AddCategory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		var req addLinkedEligibleCategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.audit(r, admin.AuditLinkedEligibleCategoryAdd, childID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		link := &catalog.LinkedEligibleCategory{
			ChildProductID: childID,
			CategoryID:     req.CategoryID,
		}
		if err := h.rules.AddCategory(r.Context(), link); err != nil {
			h.audit(r, admin.AuditLinkedEligibleCategoryAdd, childID, map[string]interface{}{
				"category_id": strings.TrimSpace(req.CategoryID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		dto := h.categoryDTOFrom(r.Context(), *link)
		h.audit(r, admin.AuditLinkedEligibleCategoryAdd, childID, map[string]interface{}{
			"category_id": dto.CategoryID,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"category": dto})
	}
}

// RemoveCategory handles DELETE /api/v1/admin/products/{id}/linked-eligible-categories/{categoryId}.
func (h *LinkedEligibilityAdminHandler) RemoveCategory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childID := strings.TrimSpace(r.PathValue("id"))
		categoryID := strings.TrimSpace(r.PathValue("categoryId"))
		details := map[string]interface{}{"category_id": categoryID}
		if err := h.rules.RemoveCategory(r.Context(), childID, categoryID); err != nil {
			h.audit(r, admin.AuditLinkedEligibleCategoryRemove, childID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditLinkedEligibleCategoryRemove, childID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

func parentProductIDs(rows []catalog.LinkedEligibleParent) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ParentProductID)
	}
	return ids
}

func linkedParentDTOFrom(row catalog.LinkedEligibleParent, parent *catalog.Product) linkedEligibleParentDTO {
	dto := linkedEligibleParentDTO{
		ParentProductID: row.ParentProductID,
		CreatedAt:       row.CreatedAt,
	}
	if parent != nil {
		dto.Name = parent.Name
		dto.Slug = parent.Slug
		dto.Type = string(parent.Type)
	}
	return dto
}

func (h *LinkedEligibilityAdminHandler) categoryDTOFrom(ctx context.Context, row catalog.LinkedEligibleCategory) linkedEligibleCategoryDTO {
	dto := linkedEligibleCategoryDTO{
		CategoryID: row.CategoryID,
		CreatedAt:  row.CreatedAt,
	}
	cat, err := h.categories.FindByID(ctx, row.CategoryID)
	if err != nil || cat == nil {
		return dto
	}
	dto.Name = cat.Name
	dto.Slug = cat.Slug
	return dto
}

func (h *LinkedEligibilityAdminHandler) productsByID(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	out := map[string]*catalog.Product{}
	if len(ids) == 0 {
		return out, nil
	}
	if lister, ok := h.products.(productIDLister); ok {
		byID, err := lister.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("linked eligibility: list parents: %w", err)
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
		product, err := h.products.FindByID(ctx, id)
		if err != nil || product == nil {
			continue
		}
		out[id] = product
	}
	return out, nil
}

func (h *LinkedEligibilityAdminHandler) audit(r *http.Request, action admin.AuditAction, childID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "product",
		ResourceID:   childID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}
