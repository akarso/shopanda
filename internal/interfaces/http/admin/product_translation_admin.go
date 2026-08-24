package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// productTranslatableFields are the product fields that carry per-language
// translations. They mirror the translatable fields declared in the product
// form schema (product_schema.go).
var productTranslatableFields = []string{"name", "description"}

func isProductTranslatableField(field string) bool {
	for _, f := range productTranslatableFields {
		if f == field {
			return true
		}
	}
	return false
}

// ProductTranslationAdminHandler serves per-language product translation
// read/write endpoints scoped to the active admin language context.
type ProductTranslationAdminHandler struct {
	products     catalog.ProductRepository
	translations translation.ContentTranslationRepository
	auditor      *admin.Auditor
	log          logger.Logger
}

// NewProductTranslationAdminHandler creates a ProductTranslationAdminHandler.
func NewProductTranslationAdminHandler(products catalog.ProductRepository, translations translation.ContentTranslationRepository, auditor *admin.Auditor, log logger.Logger) *ProductTranslationAdminHandler {
	if products == nil {
		panic("http: product translation handler: product repository must not be nil")
	}
	if translations == nil {
		panic("http: product translation handler: translation repository must not be nil")
	}
	if auditor == nil {
		panic("http: product translation handler: auditor must not be nil")
	}
	if log == nil {
		log = logger.New("warn")
	}
	return &ProductTranslationAdminHandler{products: products, translations: translations, auditor: auditor, log: log}
}

type updateProductTranslationRequest struct {
	Entries map[string]string `json:"entries"`
}

func productTranslationFieldScopes() map[string]string {
	out := make(map[string]string, len(productTranslatableFields))
	for _, f := range productTranslatableFields {
		out[f] = "translatable"
	}
	return out
}

// Get handles GET /api/v1/admin/products/{id}/translations.
// Reads translated values for the active language scope.
func (h *ProductTranslationAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := strings.TrimSpace(r.PathValue("id"))
		language := resolveLanguageScopeID(r)
		if pid == "" {
			h.audit(r, admin.AuditProductTranslationRead, pid, nil, apperror.Validation("product id is required"))
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}
		if language == "" {
			h.audit(r, admin.AuditProductTranslationRead, pid, nil, apperror.Validation("language context is required"))
			httpshared.JSONError(w, apperror.Validation("language context is required"))
			return
		}

		product, err := h.products.FindByID(r.Context(), pid)
		if err != nil {
			h.audit(r, admin.AuditProductTranslationRead, pid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			h.audit(r, admin.AuditProductTranslationRead, pid, nil, apperror.NotFound("product not found"))
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		translations, err := h.translations.FindByEntityAndLanguage(r.Context(), pid, language)
		if err != nil {
			h.audit(r, admin.AuditProductTranslationRead, pid, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		entries := make(map[string]string, len(productTranslatableFields))
		for _, f := range productTranslatableFields {
			entries[f] = ""
		}
		for _, t := range translations {
			if isProductTranslatableField(t.Field) {
				entries[t.Field] = t.Value
			}
		}

		h.audit(r, admin.AuditProductTranslationRead, pid, map[string]interface{}{"fields_count": len(entries)}, nil)

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"entries":      entries,
			"language":     language,
			"scope":        scopePayloadFromRequest(r),
			"field_scopes": productTranslationFieldScopes(),
		})
	}
}

// Update handles PUT /api/v1/admin/products/{id}/translations.
// Upserts translated values for the active language scope. Empty values are
// ignored (a no-op), because the content translation store has no single-field
// delete; clearing a translation is out of scope for this slice.
func (h *ProductTranslationAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := strings.TrimSpace(r.PathValue("id"))
		language := resolveLanguageScopeID(r)
		if pid == "" {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, apperror.Validation("product id is required"))
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}
		if language == "" {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, apperror.Validation("language context is required"))
			httpshared.JSONError(w, apperror.Validation("language context is required"))
			return
		}

		var req updateProductTranslationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, apperror.Validation("invalid request body"))
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if len(req.Entries) == 0 {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, apperror.Validation("entries are required"))
			httpshared.JSONError(w, apperror.Validation("entries are required"))
			return
		}
		for field := range req.Entries {
			if !isProductTranslatableField(field) {
				err := apperror.Validation("invalid translatable field: " + field)
				h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, err)
				httpshared.JSONError(w, err)
				return
			}
		}

		product, err := h.products.FindByID(r.Context(), pid)
		if err != nil {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, apperror.NotFound("product not found"))
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		written := make([]string, 0, len(req.Entries))
		entries := make(map[string]string, len(productTranslatableFields))
		for _, f := range productTranslatableFields {
			entries[f] = ""
		}
		for field, value := range req.Entries {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			entries[field] = value
			ct, err := translation.NewContentTranslation(pid, language, field, value)
			if err != nil {
				err := apperror.Validation(err.Error())
				h.audit(r, admin.AuditProductTranslationUpdate, pid, nil, err)
				httpshared.JSONError(w, err)
				return
			}
			if err := h.translations.Upsert(r.Context(), &ct); err != nil {
				h.audit(r, admin.AuditProductTranslationUpdate, pid, map[string]interface{}{"field": field}, err)
				httpshared.JSONError(w, err)
				return
			}
			written = append(written, field)
		}
		sort.Strings(written)

		h.audit(r, admin.AuditProductTranslationUpdate, pid, map[string]interface{}{"fields_written": written}, nil)

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"entries":  entries,
			"language": language,
			"scope":    scopePayloadFromRequest(r),
		})
	}
}

func (h *ProductTranslationAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "product_translation",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}
