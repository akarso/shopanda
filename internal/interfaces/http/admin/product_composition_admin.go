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

// ProductCompositionAdminHandler lists and edits bundle components and grouped
// members through BundleService and GroupedService. It does not reimplement
// composition rules.
type ProductCompositionAdminHandler struct {
	products catalog.ProductRepository
	bundles  *catalogApp.BundleService
	grouped  *catalogApp.GroupedService
	auditor  *admin.Auditor
}

// NewProductCompositionAdminHandler creates a handler over the existing
// composition services. products is used only to enrich list rows with the
// child product's name, slug, and type.
func NewProductCompositionAdminHandler(
	products catalog.ProductRepository,
	bundles *catalogApp.BundleService,
	grouped *catalogApp.GroupedService,
	auditor *admin.Auditor,
) *ProductCompositionAdminHandler {
	if products == nil {
		panic("http: product composition handler: product repository must not be nil")
	}
	if bundles == nil {
		panic("http: product composition handler: bundle service must not be nil")
	}
	if grouped == nil {
		panic("http: product composition handler: grouped service must not be nil")
	}
	if auditor == nil {
		panic("http: product composition handler: auditor must not be nil")
	}
	return &ProductCompositionAdminHandler{
		products: products,
		bundles:  bundles,
		grouped:  grouped,
		auditor:  auditor,
	}
}

type bundleComponentDTO struct {
	ComponentProductID string    `json:"component_product_id"`
	Quantity           int       `json:"quantity"`
	Required           bool      `json:"required"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	Type               string    `json:"type"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
}

type groupedMemberDTO struct {
	MemberProductID string    `json:"member_product_id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Type            string    `json:"type"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

type addBundleComponentRequest struct {
	ComponentProductID string `json:"component_product_id"`
	Quantity           int    `json:"quantity"`
	Required           *bool  `json:"required"`
}

type addGroupedMemberRequest struct {
	MemberProductID string `json:"member_product_id"`
}

// ListBundleComponents handles GET /api/v1/admin/products/{id}/bundle-components.
func (h *ProductCompositionAdminHandler) ListBundleComponents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.bundles.List(r.Context(), parentID)
		if err != nil {
			h.audit(r, admin.AuditBundleComponentList, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		children, err := h.productsByID(r.Context(), componentIDs(rows))
		if err != nil {
			h.audit(r, admin.AuditBundleComponentList, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		out := make([]bundleComponentDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, bundleDTOFrom(row, children[row.ComponentProductID]))
		}
		h.audit(r, admin.AuditBundleComponentList, parentID, map[string]interface{}{"count": len(out)}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"components": out})
	}
}

// AddBundleComponent handles POST /api/v1/admin/products/{id}/bundle-components.
// Omitted required defaults to true. Add upserts quantity and required.
func (h *ProductCompositionAdminHandler) AddBundleComponent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		var req addBundleComponentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.audit(r, admin.AuditBundleComponentAdd, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		required := true
		if req.Required != nil {
			required = *req.Required
		}
		component := &catalog.BundleComponent{
			BundleProductID:    parentID,
			ComponentProductID: req.ComponentProductID,
			Quantity:           req.Quantity,
			Required:           required,
		}
		if err := h.bundles.Add(r.Context(), component); err != nil {
			h.audit(r, admin.AuditBundleComponentAdd, parentID, map[string]interface{}{
				"component_product_id": strings.TrimSpace(req.ComponentProductID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		// The write is committed. Enrichment is best-effort: a missing or
		// unreadable child must not turn this response into an error.
		children, _ := h.productsByID(r.Context(), []string{component.ComponentProductID})
		dto := bundleDTOFrom(*component, children[component.ComponentProductID])
		h.audit(r, admin.AuditBundleComponentAdd, parentID, map[string]interface{}{
			"component_product_id": dto.ComponentProductID,
			"quantity":             dto.Quantity,
			"required":             dto.Required,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"component": dto})
	}
}

// RemoveBundleComponent handles DELETE /api/v1/admin/products/{id}/bundle-components/{componentId}.
// componentId is the component product id. Remove checks the parent type with
// one FindByID. A parent that is not a bundle is 422. A missing link is 404.
func (h *ProductCompositionAdminHandler) RemoveBundleComponent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		componentID := strings.TrimSpace(r.PathValue("componentId"))
		details := map[string]interface{}{"component_product_id": componentID}
		if err := h.bundles.Remove(r.Context(), parentID, componentID); err != nil {
			h.audit(r, admin.AuditBundleComponentRemove, parentID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditBundleComponentRemove, parentID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

// ListGroupedMembers handles GET /api/v1/admin/products/{id}/grouped-members.
func (h *ProductCompositionAdminHandler) ListGroupedMembers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		rows, err := h.grouped.List(r.Context(), parentID)
		if err != nil {
			h.audit(r, admin.AuditGroupedMemberList, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		children, err := h.productsByID(r.Context(), memberIDs(rows))
		if err != nil {
			h.audit(r, admin.AuditGroupedMemberList, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		out := make([]groupedMemberDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, memberDTOFrom(row, children[row.MemberProductID]))
		}
		h.audit(r, admin.AuditGroupedMemberList, parentID, map[string]interface{}{"count": len(out)}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"members": out})
	}
}

// AddGroupedMember handles POST /api/v1/admin/products/{id}/grouped-members.
// A duplicate link is an insert-ignore success.
func (h *ProductCompositionAdminHandler) AddGroupedMember() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		var req addGroupedMemberRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.audit(r, admin.AuditGroupedMemberAdd, parentID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		member := &catalog.GroupedMember{
			GroupProductID:  parentID,
			MemberProductID: req.MemberProductID,
		}
		if err := h.grouped.Add(r.Context(), member); err != nil {
			h.audit(r, admin.AuditGroupedMemberAdd, parentID, map[string]interface{}{
				"member_product_id": strings.TrimSpace(req.MemberProductID),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		children, _ := h.productsByID(r.Context(), []string{member.MemberProductID})
		dto := memberDTOFrom(*member, children[member.MemberProductID])
		h.audit(r, admin.AuditGroupedMemberAdd, parentID, map[string]interface{}{
			"member_product_id": dto.MemberProductID,
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"member": dto})
	}
}

// RemoveGroupedMember handles DELETE /api/v1/admin/products/{id}/grouped-members/{memberId}.
// memberId is the member product id. Remove checks the parent type with one
// FindByID. A parent that is not grouped is 422. A missing link is 404.
func (h *ProductCompositionAdminHandler) RemoveGroupedMember() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentID := strings.TrimSpace(r.PathValue("id"))
		memberID := strings.TrimSpace(r.PathValue("memberId"))
		details := map[string]interface{}{"member_product_id": memberID}
		if err := h.grouped.Remove(r.Context(), parentID, memberID); err != nil {
			h.audit(r, admin.AuditGroupedMemberRemove, parentID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditGroupedMemberRemove, parentID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"removed": true})
	}
}

// productIDLister is the optional batch read on ProductRepository. Postgres
// implements it. Callers that only have FindByID still enrich one id at a time.
type productIDLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

func componentIDs(rows []catalog.BundleComponent) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ComponentProductID)
	}
	return ids
}

func memberIDs(rows []catalog.GroupedMember) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.MemberProductID)
	}
	return ids
}

func bundleDTOFrom(row catalog.BundleComponent, child *catalog.Product) bundleComponentDTO {
	dto := bundleComponentDTO{
		ComponentProductID: row.ComponentProductID,
		Quantity:           row.Quantity,
		Required:           row.Required,
		CreatedAt:          row.CreatedAt,
	}
	if child != nil {
		dto.Name = child.Name
		dto.Slug = child.Slug
		dto.Type = string(child.Type)
	}
	return dto
}

func memberDTOFrom(row catalog.GroupedMember, child *catalog.Product) groupedMemberDTO {
	dto := groupedMemberDTO{
		MemberProductID: row.MemberProductID,
		CreatedAt:       row.CreatedAt,
	}
	if child != nil {
		dto.Name = child.Name
		dto.Slug = child.Slug
		dto.Type = string(child.Type)
	}
	return dto
}

// productsByID loads children for list and add responses.
// A missing id leaves name, slug, and type empty. A FindByID error for one
// child does the same and does not fail the caller. ListByIDs errors are
// returned so list handlers fail the request instead of blanking every child.
// Add handlers ignore the error: enrichment stays best-effort after a commit.
func (h *ProductCompositionAdminHandler) productsByID(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	out := map[string]*catalog.Product{}
	if len(ids) == 0 {
		return out, nil
	}
	if lister, ok := h.products.(productIDLister); ok {
		byID, err := lister.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("product composition: list children: %w", err)
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

func (h *ProductCompositionAdminHandler) audit(r *http.Request, action admin.AuditAction, parentID string, details map[string]interface{}, err error) {
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
		ResourceID:   parentID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}
