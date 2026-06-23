package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AuditLogAdminHandler serves read-only admin audit log endpoints.
type AuditLogAdminHandler struct {
	repo    domainadmin.AuditLogRepository
	auditor *adminapp.Auditor
}

// NewAuditLogAdminHandler creates an AuditLogAdminHandler.
func NewAuditLogAdminHandler(repo domainadmin.AuditLogRepository, auditor *adminapp.Auditor) *AuditLogAdminHandler {
	if repo == nil {
		panic("http: audit log repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &AuditLogAdminHandler{repo: repo, auditor: auditor}
}

type auditLogItemResp struct {
	ID           string                 `json:"id"`
	CreatedAt    time.Time              `json:"created_at"`
	AdminID      string                 `json:"admin_id"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Result       string                 `json:"result"`
	Error        string                 `json:"error,omitempty"`
	StoreID      string                 `json:"store_id,omitempty"`
	Language     string                 `json:"language,omitempty"`
	Currency     string                 `json:"currency,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// List handles GET /api/v1/admin/audit.
func (h *AuditLogAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		filter := domainadmin.AuditLogFilter{
			Action:       strings.TrimSpace(r.URL.Query().Get("action")),
			ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
			ResourceID:   strings.TrimSpace(r.URL.Query().Get("resource_id")),
			Offset:       offset,
			Limit:        limit,
		}
		if from, err := parseAuditTimeFilter(r.URL.Query().Get("from")); err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		} else if from != nil {
			filter.From = from
		}
		if to, err := parseAuditTimeFilter(r.URL.Query().Get("to")); err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		} else if to != nil {
			filter.To = to
		}

		entries, err := h.repo.List(r.Context(), filter)
		if err != nil {
			h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
				AdminID:      adminIDFromRequest(r),
				Action:       adminapp.AuditLogList,
				ResourceType: "audit_log",
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		items := make([]auditLogItemResp, 0, len(entries))
		for _, entry := range entries {
			items = append(items, auditLogItemResp{
				ID:           entry.ID,
				CreatedAt:    entry.CreatedAt,
				AdminID:      entry.AdminID,
				Action:       entry.Action,
				ResourceType: entry.ResourceType,
				ResourceID:   entry.ResourceID,
				Result:       entry.Result,
				Error:        entry.ErrorMessage,
				StoreID:      entry.StoreID,
				Language:     entry.Language,
				Currency:     entry.Currency,
				Metadata:     entry.Metadata,
			})
		}

		h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
			AdminID:      adminIDFromRequest(r),
			Action:       adminapp.AuditLogList,
			ResourceType: "audit_log",
			Result:       "success",
			Details: map[string]interface{}{
				"offset": offset,
				"limit":  limit,
				"count":  len(items),
			},
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"entries": items,
			"offset":  offset,
			"limit":   limit,
		})
	}
}

func parseAuditTimeFilter(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	return nil, fmt.Errorf("invalid time filter %q (use RFC3339 or YYYY-MM-DD)", raw)
}
