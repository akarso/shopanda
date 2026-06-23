package admin

import (
	"context"
	"fmt"

	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// AuditAction represents an admin action that should be logged for compliance and security.
type AuditAction string

const (
	// Order operations
	AuditOrderList         AuditAction = "order.list"
	AuditOrderRead         AuditAction = "order.read"
	AuditOrderUpdate       AuditAction = "order.update"
	AuditOrderStatusChange AuditAction = "order.status_change"

	// Future actions (for extensibility)
	AuditProductCreate AuditAction = "product.create"
	AuditProductRead   AuditAction = "product.read"
	AuditProductUpdate AuditAction = "product.update"

	// Scoped catalog editing (PR-E catalog slice)
	AuditProductTranslationRead   AuditAction = "product.translation.read"
	AuditProductTranslationUpdate AuditAction = "product.translation.update"
	AuditPriceRead                AuditAction = "price.read"
	AuditPriceUpdate              AuditAction = "price.update"

	// Scoped content editing (PR-E content slice)
	AuditPageRead   AuditAction = "page.read"
	AuditPageCreate AuditAction = "page.create"
	AuditPageUpdate AuditAction = "page.update"
	AuditPageDelete AuditAction = "page.delete"

	// Category operations
	AuditCategoryCreate          AuditAction = "category.create"
	AuditCategoryUpdate          AuditAction = "category.update"
	AuditCategoryDelete          AuditAction = "category.delete"
	AuditCategoryProductAssign   AuditAction = "category.product_assign"
	AuditCategoryProductUnassign AuditAction = "category.product_unassign"

	// Store operations
	AuditStoreCreate AuditAction = "store.create"
	AuditStoreUpdate AuditAction = "store.update"

	// Media operations
	AuditMediaUpload AuditAction = "media.upload"
	AuditMediaDelete AuditAction = "media.delete"

	AuditStatsRead      AuditAction = "stats.read"
	AuditCustomerDelete AuditAction = "customer.delete"
	AuditCustomerRead   AuditAction = "customer.read"
	AuditCustomerRevoke AuditAction = "customer.revoke_sessions"
	AuditSettingsRead   AuditAction = "settings.read"
	AuditSettingsChange AuditAction = "settings.change"

	// Coupon operations
	AuditCouponRead   AuditAction = "coupon.read"
	AuditCouponCreate AuditAction = "coupon.create"
	AuditCouponUpdate AuditAction = "coupon.update"
	AuditCouponDelete AuditAction = "coupon.delete"

	// Promotion operations
	AuditPromotionRead   AuditAction = "promotion.read"
	AuditPromotionCreate AuditAction = "promotion.create"
	AuditPromotionUpdate AuditAction = "promotion.update"
	AuditPromotionDelete AuditAction = "promotion.delete"

	// Stock operations
	AuditStockRead   AuditAction = "stock.read"
	AuditStockUpdate AuditAction = "stock.update"

	// Attribute operations
	AuditAttributeRead   AuditAction = "attribute.read"
	AuditAttributeCreate AuditAction = "attribute.create"
	AuditAttributeUpdate AuditAction = "attribute.update"
	AuditAttributeDelete AuditAction = "attribute.delete"

	// Attribute group operations
	AuditAttributeGroupRead   AuditAction = "attribute_group.read"
	AuditAttributeGroupCreate AuditAction = "attribute_group.create"
	AuditAttributeGroupUpdate AuditAction = "attribute_group.update"
	AuditAttributeGroupDelete AuditAction = "attribute_group.delete"

	AuditLogList AuditAction = "audit.list"
)

// AuditEntry represents a single admin audit log entry.
type AuditEntry struct {
	AdminID      string                 `json:"admin_id"`
	Action       AuditAction            `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Details      map[string]interface{} `json:"details,omitempty"`
	Result       string                 `json:"result"` // "success" or "error"
	Error        string                 `json:"error,omitempty"`
}

// Auditor logs admin actions with context.
type Auditor struct {
	log  logger.Logger
	repo domainadmin.AuditLogRepository
}

// NewAuditor creates an Auditor that logs to the provided logger.
func NewAuditor(log logger.Logger) *Auditor {
	if log == nil {
		panic("auditor: logger must not be nil")
	}
	return &Auditor{log: log}
}

// SetAuditLogRepository enables best-effort persistence of audit entries.
func (a *Auditor) SetAuditLogRepository(repo domainadmin.AuditLogRepository) {
	if a == nil {
		return
	}
	a.repo = repo
}

// LogAction records an admin action to the audit log.
func (a *Auditor) LogAction(ctx context.Context, entry AuditEntry) {
	adminID := entry.AdminID
	if adminID == "" {
		adminID = "unknown"
	}
	result := entry.Result
	if result == "" {
		result = "unknown"
	}

	// Always include the action context and results in logs.
	// In the future, this can be extended to write to a dedicated audit table.
	logFields := map[string]interface{}{
		"admin_id":      adminID,
		"action":        entry.Action,
		"resource_type": entry.ResourceType,
		"resource_id":   entry.ResourceID,
		"result":        result,
	}
	if entry.Error != "" {
		logFields["error"] = entry.Error
	}
	details := make(map[string]interface{}, len(entry.Details))
	for k, v := range entry.Details {
		details[k] = v
	}
	for k, v := range details {
		logFields["detail_"+k] = v
	}

	// Log sensitive operations as warnings for immediate alertability.
	if result == "error" {
		a.log.Warn("admin.action.failed", logFields)
	} else {
		a.log.Info("admin.action", logFields)
	}

	a.persistEntry(ctx, entry)
}

func (a *Auditor) persistEntry(ctx context.Context, entry AuditEntry) {
	if a == nil || a.repo == nil {
		return
	}
	record := auditRecordFromEntry(entry)
	if err := a.repo.Insert(ctx, record); err != nil {
		a.log.Warn("admin.audit.persist_failed", map[string]interface{}{
			"admin_id":      record.AdminID,
			"action":        record.Action,
			"resource_type": record.ResourceType,
			"resource_id":   record.ResourceID,
			"error":         err.Error(),
		})
	}
}

func auditRecordFromEntry(entry AuditEntry) domainadmin.AuditLogRecord {
	adminID := entry.AdminID
	if adminID == "" {
		adminID = "unknown"
	}
	result := entry.Result
	if result == "" {
		result = "unknown"
	}

	metadata := make(map[string]interface{})
	storeID, language, currency := "", "", ""
	for k, v := range entry.Details {
		switch k {
		case "store_id":
			storeID = fmt.Sprint(v)
		case "language":
			language = fmt.Sprint(v)
		case "currency":
			currency = fmt.Sprint(v)
		default:
			metadata[k] = v
		}
	}

	return domainadmin.AuditLogRecord{
		AdminID:      adminID,
		Action:       string(entry.Action),
		ResourceType: entry.ResourceType,
		ResourceID:   entry.ResourceID,
		Result:       result,
		ErrorMessage: entry.Error,
		StoreID:      storeID,
		Language:     language,
		Currency:     currency,
		Metadata:     metadata,
	}
}

// LogOrderRead logs a read access to an order (for audit trail).
func (a *Auditor) LogOrderRead(ctx context.Context, adminID, orderID string) {
	a.LogAction(ctx, AuditEntry{
		AdminID:      adminID,
		Action:       AuditOrderRead,
		ResourceType: "order",
		ResourceID:   orderID,
		Result:       "success",
	})
}

// LogOrderListAccess logs a list operation on orders (for audit trail).
func (a *Auditor) LogOrderListAccess(ctx context.Context, adminID string, offset, limit int) {
	a.LogAction(ctx, AuditEntry{
		AdminID:      adminID,
		Action:       AuditOrderList,
		ResourceType: "orders",
		Result:       "success",
		Details: map[string]interface{}{
			"offset": offset,
			"limit":  limit,
		},
	})
}

// LogOrderUpdate logs an order update operation (for audit trail and compliance).
func (a *Auditor) LogOrderUpdate(ctx context.Context, adminID, orderID string, oldStatus, newStatus string) {
	a.LogAction(ctx, AuditEntry{
		AdminID:      adminID,
		Action:       AuditOrderStatusChange,
		ResourceType: "order",
		ResourceID:   orderID,
		Result:       "success",
		Details: map[string]interface{}{
			"old_status": oldStatus,
			"new_status": newStatus,
		},
	})
}

// LogOrderUpdateError logs a failed order update attempt (for security audit).
func (a *Auditor) LogOrderUpdateError(ctx context.Context, adminID, orderID string, errMsg string) {
	a.LogAction(ctx, AuditEntry{
		AdminID:      adminID,
		Action:       AuditOrderUpdate,
		ResourceType: "order",
		ResourceID:   orderID,
		Result:       "error",
		Error:        errMsg,
	})
}
