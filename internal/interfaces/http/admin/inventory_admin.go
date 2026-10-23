package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// txStockRepo binds stock reads and writes to the adjust transaction.
type txStockRepo interface {
	inventory.StockRepository
	WithTx(tx *sql.Tx) inventory.StockRepository
}

// InventoryAdminHandler serves inventory admin endpoints.
type InventoryAdminHandler struct {
	stock     inventory.StockRepository
	variants  catalog.VariantRepository
	products  txProductRepo
	txStarter txStarter
	auditor   *admin.Auditor
	bus       *event.Bus // optional; nil (the default) skips publishing — see SetBus.
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

// WithTxStarter wires the transaction beginner used by Adjust. The handler
// locks the product row and writes stock on that same transaction. A nil
// starter fails closed when Adjust runs. *sql.DB satisfies this.
func (h *InventoryAdminHandler) WithTxStarter(starter txStarter) *InventoryAdminHandler {
	h.txStarter = starter
	return h
}

// SetBus enables publishing inventory.EventStockUpdated on every successful
// Adjust (PR-1036: the search index's on-save subscriber listens for it).
// Left unset (nil), Adjust works exactly as before — no event, no error —
// matching how NewAuditor's own optional wiring (SetAuditLogRepository)
// behaves, and keeping every existing constructor/test call site working
// unchanged.
func (h *InventoryAdminHandler) SetBus(bus *event.Bus) {
	h.bus = bus
}

// WithProducts wires the product repository Adjust locks before SetStock.
// Nil panics. Adjust fails closed when this was not called, and when the
// repository cannot bind to the adjust transaction.
func (h *InventoryAdminHandler) WithProducts(products txProductRepo) *InventoryAdminHandler {
	if products == nil {
		panic("InventoryAdminHandler: products must not be nil")
	}
	h.products = products
	return h
}

type adjustStockRequest struct {
	Quantity *int `json:"quantity"`
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

// adjustStock locks the product row, rejects a dynamic bundle, and writes
// stock on that same transaction. Product row first, then the stock row,
// matching the mode-flip lock order.
func (h *InventoryAdminHandler) adjustStock(ctx context.Context, variant *catalog.Variant, entry inventory.StockEntry) (before int, err error) {
	if h.txStarter == nil {
		return 0, apperror.Internal("inventory admin: transaction starter is not configured")
	}
	if h.products == nil {
		return 0, apperror.Internal("inventory admin: product repository is not configured")
	}
	txStock, ok := h.stock.(txStockRepo)
	if !ok {
		return 0, fmt.Errorf("inventory admin: stock repository does not support transactions")
	}

	tx, err := h.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("inventory admin: begin tx: %w", err)
	}
	defer tx.Rollback()

	locker, ok := h.products.WithTx(tx).(lockingProductRepo)
	if !ok {
		return 0, fmt.Errorf("inventory admin: product repository does not support row locks")
	}
	productType, _, stockMode, _, err := locker.LockTypeAndModes(ctx, variant.ProductID)
	if err != nil {
		return 0, err
	}
	if productType == catalog.TypeBundle && stockMode == catalog.StockModeDynamic {
		return 0, apperror.Validation(catalog.DynamicBundleStockRejectedMessage)
	}

	boundStock := txStock.WithTx(tx)
	current, err := boundStock.GetStock(ctx, variant.ID)
	if err != nil {
		return 0, fmt.Errorf("inventory admin: get stock: %w", err)
	}
	if err := boundStock.SetStock(ctx, &entry); err != nil {
		return current.Quantity, fmt.Errorf("inventory admin: set stock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return current.Quantity, fmt.Errorf("inventory admin: commit: %w", err)
	}
	return current.Quantity, nil
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
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			h.audit(r, admin.AuditStockRead, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		search := strings.TrimSpace(r.URL.Query().Get("search"))

		items, err := h.stock.ListInventory(r.Context(), offset, limit, search)
		if err != nil {
			h.audit(r, admin.AuditStockRead, "", map[string]interface{}{"search": search}, err)
			httpshared.JSONError(w, apperror.Internal("inventory list failed"))
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
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
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
			httpshared.JSONError(w, verr)
			return
		}

		var req adjustStockRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditStockUpdate, variantID, nil, verr)
			httpshared.JSONError(w, verr)
			return
		}
		if req.Quantity == nil {
			verr := apperror.Validation("quantity is required")
			h.audit(r, admin.AuditStockUpdate, variantID, nil, verr)
			httpshared.JSONError(w, verr)
			return
		}
		quantity := *req.Quantity

		variant, err := h.variants.FindByID(r.Context(), variantID)
		if err != nil {
			h.audit(r, admin.AuditStockUpdate, variantID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if variant == nil {
			verr := apperror.NotFound("variant not found")
			h.audit(r, admin.AuditStockUpdate, variantID, nil, verr)
			httpshared.JSONError(w, verr)
			return
		}
		entry, err := inventory.NewStockEntry(variantID, quantity)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
				"quantity_after": quantity,
			}, verr)
			httpshared.JSONError(w, verr)
			return
		}

		beforeQty, err := h.adjustStock(r.Context(), variant, entry)
		if err != nil {
			details := map[string]interface{}{"quantity_after": quantity}
			if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
				h.audit(r, admin.AuditStockUpdate, variantID, nil, err)
				httpshared.JSONError(w, err)
				return
			}
			h.audit(r, admin.AuditStockUpdate, variantID, details, err)
			if strings.Contains(err.Error(), "get stock") {
				httpshared.JSONError(w, apperror.Internal("inventory read failed"))
				return
			}
			httpshared.JSONError(w, apperror.Internal("inventory update failed"))
			return
		}

		h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
			"sku":             variant.SKU,
			"quantity_before": beforeQty,
			"quantity_after":  entry.Quantity,
		}, nil)

		if h.bus != nil {
			_ = h.bus.Publish(r.Context(), event.New(inventory.EventStockUpdated, "inventory.admin", inventory.StockUpdatedData{
				ProductID: variant.ProductID,
				VariantID: variant.ID,
				SKU:       variant.SKU,
				OnHand:    inventory.Qty(entry.Quantity),
			}))
		}

		item, err := h.stock.GetInventoryItem(r.Context(), variantID)
		if err != nil {
			h.audit(r, admin.AuditStockUpdate, variantID, map[string]interface{}{
				"sku":             variant.SKU,
				"quantity_before": beforeQty,
				"quantity_after":  entry.Quantity,
			}, err)
			httpshared.JSONError(w, apperror.Internal("inventory read failed"))
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"item":                toInventoryItemResponse(item),
			"low_stock_threshold": admin.LowStockThreshold,
		})
	}
}
