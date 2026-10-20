package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ProductAdminHandler serves product write endpoints.
type ProductAdminHandler struct {
	repo    catalog.ProductRepository
	bus     *event.Bus
	auditor *admin.Auditor
	log     logger.Logger

	// facts + opts + enforced back the "visibility" payload on Get. Nil facts
	// (the default — WithVisibilityFacts not called) omits the
	// key entirely; existing callers see no behavior change. enforced mirrors
	// catalog.visibility.enforce; the GET payload reports it as-is (the SPA
	// shows a section-level "Enforcement off — preview" caption when it is
	// false, instead of annotating every individual badge).
	facts    catalog.VisibilityFactsAdminSource
	opts     catalog.VisibilityOptions
	enforced bool
}

type productCategoryAssignmentReader interface {
	ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error)
}

// adminAxisExplanation is the GET /admin/products/{id} wire DTO for one
// visibility axis explanation. catalog.AxisExplanation carries
// no JSON tags — the domain stays HTTP-agnostic — so this type owns the
// snake_case shape. FailureCodes is always emitted (never omitted, even
// empty) so OpenAPI examples and the actual wire response match exactly on
// every axis, forced or auto.
type adminAxisExplanation struct {
	Mode         string   `json:"mode"`
	Override     bool     `json:"override"`
	Resolved     bool     `json:"resolved"`
	Effective    bool     `json:"effective"`
	FailureCodes []string `json:"failure_codes"`
}

func newAdminAxisExplanation(exp catalog.AxisExplanation) adminAxisExplanation {
	codes := exp.FailureCodes
	if codes == nil {
		codes = []string{}
	}
	return adminAxisExplanation{
		Mode:         string(exp.Mode),
		Override:     exp.Override,
		Resolved:     exp.Resolved,
		Effective:    exp.Effective,
		FailureCodes: codes,
	}
}

// NewProductAdminHandler creates a ProductAdminHandler.
func NewProductAdminHandler(repo catalog.ProductRepository, bus *event.Bus) *ProductAdminHandler {
	log := logger.New("info")
	return NewProductAdminHandlerWithAuditor(repo, bus, admin.NewAuditor(log), log)
}

// NewProductAdminHandlerWithAuditor creates a ProductAdminHandler with a custom auditor.
func NewProductAdminHandlerWithAuditor(repo catalog.ProductRepository, bus *event.Bus, auditor *admin.Auditor, log logger.Logger) *ProductAdminHandler {
	if log == nil {
		log = logger.New("warn")
	}
	return &ProductAdminHandler{repo: repo, bus: bus, auditor: auditor, log: log}
}

// WithVisibilityFacts wires AutoBasis facts for the GET "visibility" payload
// (PR-1059). facts loads regardless of product Status — unlike the public
// VisibilityFactsSource gate, the admin GUI must explain axes for
// draft/archived products too. enforced mirrors catalog.visibility.enforce
// (whether the public axis gate is actually on) so the GET payload can tell
// the SPA whether "effective" reflects reality or a hypothetical. Returns the
// receiver for chaining, matching storefront handlers' WithVisibilityFacts
// convention.
func (h *ProductAdminHandler) WithVisibilityFacts(facts catalog.VisibilityFactsAdminSource, opts catalog.VisibilityOptions, enforced bool) *ProductAdminHandler {
	h.facts = facts
	h.opts = opts
	h.enforced = enforced
	return h
}

func (h *ProductAdminHandler) getAdminID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil || ac.AdminID == "" {
		id := auth.IdentityFrom(r.Context())
		if id.Role == identity.RoleAdmin {
			h.log.Warn("admin.context.missing", map[string]interface{}{
				"component":   "product_admin.getAdminID",
				"path":        r.URL.Path,
				"admin_id":    "system",
				"identity_id": id.UserID,
				"reason":      "admin context missing or empty",
			})
		}
		return "system"
	}
	return ac.AdminID
}

func productAdminScopeDetailsFromRequest(r *http.Request) map[string]interface{} {
	return fullAdminScopeDetailsFromRequest(r)
}

// List handles GET /api/v1/admin/products.
func (h *ProductAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		adminID := h.getAdminID(r)
		filter := catalog.ListFilter{Offset: offset, Limit: limit}

		rawType := strings.TrimSpace(r.URL.Query().Get("type"))
		if rawType != "" {
			typ := catalog.Type(rawType)
			if !typ.IsValid() {
				msg := catalog.InvalidTypeMessage(typ)
				details := productAdminScopeDetailsFromRequest(r)
				details["offset"] = offset
				details["limit"] = limit
				details["type"] = rawType
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductRead,
					ResourceType: "products",
					Details:      details,
					Result:       "error",
					Error:        msg,
				})
				httpshared.JSONError(w, apperror.Validation(msg))
				return
			}
			filter.Type = typ
		}

		products, err := h.repo.List(r.Context(), filter)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			details["offset"] = offset
			details["limit"] = limit
			if filter.Type != "" {
				details["type"] = string(filter.Type)
			}
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "products",
				Details:      details,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}
		if products == nil {
			products = []catalog.Product{}
		}

		details := productAdminScopeDetailsFromRequest(r)
		details["offset"] = offset
		details["limit"] = limit
		if filter.Type != "" {
			details["type"] = string(filter.Type)
		}
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "products",
			Result:       "success",
			Details:      details,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"products": products,
		})
	}
}

// Get handles GET /api/v1/admin/products/{id}.
func (h *ProductAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		assignments, ok := h.repo.(productCategoryAssignmentReader)
		if !ok {
			err := apperror.Internal("product repository missing category lookup capability")
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product repository missing category lookup capability",
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}

		categoryIDs, err := assignments.ListCategoryIDsByProduct(r.Context(), pid)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if categoryIDs == nil {
			categoryIDs = []string{}
		}

		// visibility is informational (not a gate — see WithVisibilityFacts)
		// and must never turn a facts-load failure into a 500 for the whole
		// product read: a transient facts error degrades to a logged warning
		// and a "visibility_error" string in the response, with "visibility"
		// omitted. A missing snapshot (snap, ok := snaps[pid] is false, no
		// error) also sets "visibility_error": this repo's facts query
		// joins live product/variant/stock/price rows (no async "first
		// refresh" step), so the only way FindByID above finds the product
		// but the facts query below does not is a concurrent delete between
		// the two reads — a real (if rare) failure to explain, not a
		// "nothing to show yet" pending state like a true pre-save create.
		var visibilityPayload map[string]interface{}
		var visibilityErr string
		if h.facts != nil {
			snaps, err := h.facts.LoadManyAnyStatus(r.Context(), []string{pid})
			if err != nil {
				h.log.Warn("product_admin.visibility_facts.load_failed", map[string]interface{}{
					"component":  "product_admin.Get",
					"product_id": pid,
					"error":      err.Error(),
				})
				visibilityErr = "visibility facts unavailable"
			} else if snap, ok := snaps[pid]; ok {
				// Prefer the loaded product row's Status/VisibilityModes over
				// the snapshot's — they can diverge if the product changed
				// between the FindByID above and this LoadManyAnyStatus call.
				autoBasis, codes, axesExp := catalog.ExplainAxes(product.VisibilityModes, product.Status, snap.Inputs, h.opts)
				if codes == nil {
					codes = []string{}
				}
				// input_failure_codes excludes AutoBasisCodeStatusInactive so
				// the SPA can recompute auto_basis/failure_codes when the form
				// status changes. failure_codes stays the full list for the
				// product's saved status.
				inputCodes := catalog.AutoBasisInputFailureCodes(snap.Inputs, h.opts)
				if inputCodes == nil {
					inputCodes = []string{}
				}
				visibilityPayload = map[string]interface{}{
					"enforced":            h.enforced,
					"status":              string(product.Status),
					"auto_basis":          autoBasis,
					"failure_codes":       codes,
					"input_failure_codes": inputCodes,
					"axes": map[string]interface{}{
						"catalog":      newAdminAxisExplanation(axesExp.Catalog),
						"search":       newAdminAxisExplanation(axesExp.Search),
						"individually": newAdminAxisExplanation(axesExp.Individually),
						"purchasable":  newAdminAxisExplanation(axesExp.Purchasable),
					},
				}
			} else {
				h.log.Warn("product_admin.visibility_facts.snapshot_missing", map[string]interface{}{
					"component":  "product_admin.Get",
					"product_id": pid,
				})
				visibilityErr = "product not found in visibility facts"
			}
		}

		details := productAdminScopeDetailsFromRequest(r)
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      details,
		})

		resp := map[string]interface{}{
			"product":      product,
			"category_ids": categoryIDs,
		}
		if visibilityPayload != nil {
			resp["visibility"] = visibilityPayload
		}
		if visibilityErr != "" {
			resp["visibility_error"] = visibilityErr
		}
		httpshared.JSON(w, http.StatusOK, resp)
	}
}

// visibilityModesRequest is the JSON shape for a *partial* update of
// catalog.VisibilityAxes on create/update requests. Every field
// is an optional pointer: an omitted axis keeps whatever mergeVisibilityModes
// was given as a base (DefaultVisibilityAxes() on create, the product's
// existing modes on update) — a caller that only wants to flip one axis does
// not have to resend the other three.
type visibilityModesRequest struct {
	Catalog      *string `json:"catalog"`
	Search       *string `json:"search"`
	Individually *string `json:"individually"`
	Purchasable  *string `json:"purchasable"`
}

// mergeVisibilityModes applies only the present (non-nil) fields of req onto
// base, leaving every omitted axis untouched, then validates the merged
// result. A nil req returns base unchanged. Empty string is not a valid mode
// (caught by Validate, same as any other unrecognised value).
func mergeVisibilityModes(base catalog.VisibilityAxes, req *visibilityModesRequest) (catalog.VisibilityAxes, error) {
	if req == nil {
		return base, nil
	}
	out := base
	if req.Catalog != nil {
		out.Catalog = catalog.VisibilityMode(*req.Catalog)
	}
	if req.Search != nil {
		out.Search = catalog.VisibilityMode(*req.Search)
	}
	if req.Individually != nil {
		out.Individually = catalog.VisibilityMode(*req.Individually)
	}
	if req.Purchasable != nil {
		out.Purchasable = catalog.VisibilityMode(*req.Purchasable)
	}
	if err := out.Validate(); err != nil {
		return catalog.VisibilityAxes{}, err
	}
	return out, nil
}

// visibilityModesMap renders axes as a plain string map for audit Details
// (before/after snapshots on Update).
func visibilityModesMap(axes catalog.VisibilityAxes) map[string]string {
	return map[string]string{
		"catalog":      string(axes.Catalog),
		"search":       string(axes.Search),
		"individually": string(axes.Individually),
		"purchasable":  string(axes.Purchasable),
	}
}

// createProductRequest is the JSON body for creating a product.
type createProductRequest struct {
	Name            string                  `json:"name"`
	Slug            string                  `json:"slug"`
	Description     string                  `json:"description"`
	Type            string                  `json:"type"`
	VisibilityModes *visibilityModesRequest `json:"visibility_modes"`
	Attributes      map[string]interface{}  `json:"attributes"`
}

// updateProductRequest is the JSON body for updating a product.
type updateProductRequest struct {
	Name            *string                 `json:"name"`
	Slug            *string                 `json:"slug"`
	Description     *string                 `json:"description"`
	Status          *string                 `json:"status"`
	Type            *string                 `json:"type"`
	VisibilityModes *visibilityModesRequest `json:"visibility_modes"`
	Attributes      map[string]interface{}  `json:"attributes"`
}

// Create handles POST /api/v1/admin/products.
func (h *ProductAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := h.getAdminID(r)
		details := productAdminScopeDetailsFromRequest(r)

		var req createProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        "invalid request body",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := catalog.NewProduct(id.New(), req.Name, req.Slug)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.Description = req.Description
		if rawType := strings.TrimSpace(req.Type); rawType != "" {
			product.Type = catalog.Type(rawType)
		}
		// Omitted VisibilityModes (or an omitted axis within it) keeps
		// NewProduct's DefaultVisibilityAxes() (auto) for that axis; present
		// axes merge on top. Invalid mode values are caught here.
		modes, err := mergeVisibilityModes(product.VisibilityModes, req.VisibilityModes)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.VisibilityModes = modes
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}
		if err := product.Validate(); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.repo.Create(r.Context(), &product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				ResourceID:   product.ID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductCreate,
			ResourceType: "product",
			ResourceID:   product.ID,
			Result:       "success",
			Details:      details,
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductCreated, "product.admin", catalog.ProductCreatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
			"product": product,
		})
	}
}

// Update handles PUT /api/v1/admin/products/{id}.
func (h *ProductAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}

		var req updateProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "invalid request body",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		if req.Name != nil {
			if *req.Name == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "name must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				httpshared.JSONError(w, apperror.Validation("name must not be empty"))
				return
			}
			product.Name = *req.Name
		}
		if req.Slug != nil {
			if *req.Slug == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "slug must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				httpshared.JSONError(w, apperror.Validation("slug must not be empty"))
				return
			}
			product.Slug = *req.Slug
		}
		if req.Description != nil {
			product.Description = *req.Description
		}
		if req.Status != nil {
			s := catalog.Status(*req.Status)
			if !s.IsValid() {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "invalid status",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				httpshared.JSONError(w, apperror.Validation("invalid status"))
				return
			}
			product.Status = s
		}
		if req.Type != nil {
			rawType := strings.TrimSpace(*req.Type)
			if rawType == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "type must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				httpshared.JSONError(w, apperror.Validation("type must not be empty"))
				return
			}
			product.Type = catalog.Type(rawType)
		}
		// Omitted VisibilityModes (or an omitted axis within it) leaves the
		// product's existing modes untouched for that axis; present axes
		// merge on top. Invalid mode values are caught here. Audit
		// before/after only when the merged result differs from the stored modes.
		modesBefore := product.VisibilityModes
		modes, err := mergeVisibilityModes(product.VisibilityModes, req.VisibilityModes)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.VisibilityModes = modes
		visibilityModesChanged := req.VisibilityModes != nil && modesBefore != modes
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}
		if err := product.Validate(); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.repo.Update(r.Context(), product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, err)
			return
		}

		successDetails := productAdminScopeDetailsFromRequest(r)
		if visibilityModesChanged {
			successDetails["visibility_modes_before"] = visibilityModesMap(modesBefore)
			successDetails["visibility_modes_after"] = visibilityModesMap(modes)
		}
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductUpdate,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      successDetails,
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductUpdated, "product.admin", catalog.ProductUpdatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"product": product,
		})
	}
}
