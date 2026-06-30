package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/application/exporter"
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

		filter, err := buildAuditLogQuery(r)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		filter.Offset = offset
		filter.Limit = limit

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

// Export handles GET /api/v1/admin/audit/export.
func (h *AuditLogAdminHandler) Export() http.HandlerFunc {
	exp := exporter.NewAuditLogExporter(h.repo)
	return func(w http.ResponseWriter, r *http.Request) {
		format, err := exporter.ParseAuditExportFormat(r.URL.Query().Get("format"))
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		filter, err := buildAuditLogQuery(r)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		opts := exporter.AuditLogExportOptions{
			Format:       format,
			Action:       filter.Action,
			ResourceType: filter.ResourceType,
			ResourceID:   filter.ResourceID,
			From:         filter.From,
			To:           filter.To,
		}

		filename := "admin-audit-log.csv"
		contentType := "text/csv; charset=utf-8"
		if format == exporter.AuditLogFormatJSON {
			filename = "admin-audit-log.json"
			contentType = "application/json; charset=utf-8"
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

		body := &exportResponseWriter{ResponseWriter: w}
		result, err := exp.Export(r.Context(), body, opts)
		if err != nil {
			h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
				AdminID:      adminIDFromRequest(r),
				Action:       adminapp.AuditLogExport,
				ResourceType: "audit_log",
				Result:       "error",
				Error:        err.Error(),
			})
			if body.wrote {
				return
			}
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "audit export failed", err))
			return
		}

		h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
			AdminID:      adminIDFromRequest(r),
			Action:       adminapp.AuditLogExport,
			ResourceType: "audit_log",
			Result:       "success",
			Details: map[string]interface{}{
				"format":  string(format),
				"entries": result.Entries,
			},
		})
	}
}

type exportResponseWriter struct {
	http.ResponseWriter
	wrote bool
}

func (e *exportResponseWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		e.wrote = true
	}
	return e.ResponseWriter.Write(p)
}

func buildAuditLogQuery(r *http.Request) (domainadmin.AuditLogFilter, error) {
	filter := domainadmin.AuditLogFilter{
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		ResourceID:   strings.TrimSpace(r.URL.Query().Get("resource_id")),
	}
	from, err := parseAuditTimeFilter(r.URL.Query().Get("from"), false)
	if err != nil {
		return domainadmin.AuditLogFilter{}, err
	}
	filter.From = from
	to, err := parseAuditTimeFilter(r.URL.Query().Get("to"), true)
	if err != nil {
		return domainadmin.AuditLogFilter{}, err
	}
	filter.To = to
	return filter, nil
}

func parseAuditTimeFilter(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			end := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC)
			return &end, nil
		}
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return &start, nil
	}
	return nil, fmt.Errorf("invalid time filter %q (use RFC3339 or YYYY-MM-DD)", raw)
}
