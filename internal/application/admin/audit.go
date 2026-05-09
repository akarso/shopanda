package admin

import (
	"context"

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
	AuditProductRead    AuditAction = "product.read"
	AuditProductUpdate  AuditAction = "product.update"
	AuditCustomerRead   AuditAction = "customer.read"
	AuditSettingsRead   AuditAction = "settings.read"
	AuditSettingsChange AuditAction = "settings.change"
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
	log logger.Logger
}

// NewAuditor creates an Auditor that logs to the provided logger.
func NewAuditor(log logger.Logger) *Auditor {
	if log == nil {
		panic("auditor: logger must not be nil")
	}
	return &Auditor{log: log}
}

// LogAction records an admin action to the audit log.
func (a *Auditor) LogAction(ctx context.Context, entry AuditEntry) {
	if entry.AdminID == "" {
		entry.AdminID = "unknown"
	}
	if entry.Result == "" {
		entry.Result = "unknown"
	}

	// Always include the action context and results in logs.
	// In the future, this can be extended to write to a dedicated audit table.
	logFields := map[string]interface{}{
		"admin_id":      entry.AdminID,
		"action":        entry.Action,
		"resource_type": entry.ResourceType,
		"resource_id":   entry.ResourceID,
		"result":        entry.Result,
	}
	if entry.Error != "" {
		logFields["error"] = entry.Error
	}
	for k, v := range entry.Details {
		logFields["detail_"+k] = v
	}

	// Log sensitive operations as warnings for immediate alertability.
	if entry.Result == "error" {
		a.log.Warn("admin.action.failed", logFields)
	} else {
		a.log.Info("admin.action", logFields)
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
