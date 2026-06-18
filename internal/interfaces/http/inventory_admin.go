package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// InventoryAdminHandler serves inventory admin endpoints.
type InventoryAdminHandler struct {
	stock    inventory.StockRepository
	variants catalog.VariantRepository
	auditor  *admin.Auditor
}

// NewInventoryAdminHandler creates an InventoryAdminHandler with a default auditor.
func NewInventoryAdminHandler(stock inventory.StockRepository, variants catalog.VariantRepository) *InventoryAdminHandler {
	return NewInventoryAdminHandlerWithAuditor(stock, variants, admin.NewAuditor(logger.New("info")))
}

// NewInventoryAdminHandlerWithAuditor creates an InventoryAdminHandler with a custom auditor.
func NewInventoryAdminHandlerWithAuditor(stock inventory.StockRepository, variants catalog.VariantRepository, auditor *admin.Auditor) *InventoryAdminHandler {
	if stock == nil {
		panic("InventoryAdminHandler: stock repository must not be nil")
	}
	if variants == nil {
		panic("InventoryAdminHandler: variants repository must not be nil")
	}
	if auditor == nil {
		panic("InventoryAdminHandler: auditor must not be nil")
	}
	return &InventoryAdminHandler{stock: stock, variants: variants, auditor: auditor}
}

type adjustStockRequest struct {
	Quantity int `json:"quantity"`
}

type adminInventoryItemResponse struct {
	VariantID   string `json:"variant_id"`
	ProductID   string `json:"product_id"`
	SKU         string `json:"sku"`
	ProductName string `json:"product_name"`
	VariantName string `json:"variant_name"`
	Quantity    int    `json:"quantity"`
	Reserved    int    `json:"reserved"`
	Available   int    `json:"available"`
	LowStock    bool   `json:"low_stock"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func (h *InventoryAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "stock",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func toInventoryItemResponse(item inventory.InventoryListItem) adminInventoryItemResponse {
	lowStock := item.Quantity > 0 && item.Quantity < admin.LowStockThreshold
	resp := adminInventoryItemResponse{
		VariantID:   item.VariantID,
		ProductID:   item.ProductID,
		SKU:         item.SKU,
		ProductName: item.ProductName,
		VariantName: item.VariantName,
		Quantity:    item.Quantity,
		Reserved:    item.Reserved,
		Available:   item.Available(),
		LowStock:    lowStock,
	}
	if !item.UpdatedAt.IsZero() {
		resp.UpdatedAt = item.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// List handles GET /api/v1/admin/inventory.
func (h *InventoryAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			h.audit(r, admin.AuditStockRead, "", nil, err)
			JSONError(w, err)
			return
		}
		search := strings.TrimSpace(r.URL.Query().Get("search"))

		items, err := h.stock.ListInventory(r.Context(), offset, limit, search)
		if err != nil {
			h.audit(r, admin.AuditStockRead, "", map[string]interface{}{"search": search}, err)
			JSONError(w, apperror.Internal("inventory list failed"))
			return
		}

		resp := make([]adminInventoryItemResponse, 0, len(items))
		for _, item := range items {
			resp = append(resp, toInventoryItemResponse(item))
		}
		h.audit(r, admin.AuditStockRead, "", map[string]interface{}{
			"offset": offset,
			"limit":  limit,
			"search": search,
			"count":  len(resp),
		}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"items":               resp,
			"low_stock_threshold": admin.LowStockThreshold,
		})
	}
}

// Adjust handles PUT /api/v1/admin/inventory/{variantId}.
// Sets the absolute on-hand quantity for the variant.
func (h *InventoryAdminHandler) Adjust() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		if variantID == "" {
			verr := apperror.Validation("variant id is required")
			h.audit(r, admin.AuditStockUpdate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		var req adjustStockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditStockUpdate, variantID, nil, verr)
			JSONError(w, verr)
			return
		}

		variant, err := h.variants.FindByID(r.Context(), variantID)
		if err != nil {
			h.audit(r, admin.AuditStockUpdate, variantID, nil, err)
			JSONError(w, err)
			return
		}
		if variant == nil {
			verr := apperror.NotFound("variant not found")
			h.audit(r, admin.AuditStockUpdate, variantID, nil, verr)
			JSONError(w, verr)
			return
		}

		before, err := h.stock.GetStock(r.Context(), variantID)
		if err != nil {
			h.audit(r, admin.AuditStockUpdate, variantID, nil, err)
			JSONError(w, apperror.Internal("inventory read failed"))
			return
		}

		entry, err := inventory.NewStockEntry(variantID, req.Quantity)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
				"quantity_before": before.Quantity,
				"quantity_after":  req.Quantity,
			}, verr)
			JSONError(w, verr)
			return
		}

		if err := h.stock.SetStock(r.Context(), &entry); err != nil {
			h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
				"quantity_before": before.Quantity,
				"quantity_after":  req.Quantity,
			}, err)
			JSONError(w, apperror.Internal("inventory update failed"))
			return
		}

		h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
			"sku":             variant.SKU,
			"quantity_before": before.Quantity,
			"quantity_after":  entry.Quantity,
		}, nil)

		item := inventory.InventoryListItem{
			VariantID:   variantID,
			ProductID:   variant.ProductID,
			SKU:         variant.SKU,
			VariantName: variant.Name,
			Quantity:    entry.Quantity,
			UpdatedAt:   entry.UpdatedAt,
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"item":                toInventoryItemResponse(item),
			"low_stock_threshold": admin.LowStockThreshold,
		})
	}
}
