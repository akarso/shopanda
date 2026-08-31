package admin

import (
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	schedulerApp "github.com/akarso/shopanda/internal/application/scheduler"
	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ScheduleAdminHandler serves admin scheduler introspection and control
// endpoints (list/trigger/enable/disable) on top of the PR-1030
// scheduler.Service. Gated by the same jobs.read/jobs.write permissions as
// job admin (PR-1029) — schedules and jobs are the same conceptual
// operational surface.
type ScheduleAdminHandler struct {
	svc     *schedulerApp.Service
	auditor *adminapp.Auditor
}

// NewScheduleAdminHandler creates a ScheduleAdminHandler.
func NewScheduleAdminHandler(svc *schedulerApp.Service, auditor *adminapp.Auditor) *ScheduleAdminHandler {
	if svc == nil {
		panic("http: schedule admin service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &ScheduleAdminHandler{svc: svc, auditor: auditor}
}

func (h *ScheduleAdminHandler) audit(r *http.Request, action adminapp.AuditAction, name string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "schedule",
		ResourceID:   name,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/schedules.
func (h *ScheduleAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := h.svc.List(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditScheduleList, "", nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "list schedules failed", err))
			return
		}

		h.audit(r, adminapp.AuditScheduleList, "", map[string]interface{}{
			"count": len(entries),
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"schedules": toScheduleResponses(entries),
		})
	}
}

// Trigger handles POST /api/v1/admin/schedules/{name}/trigger.
func (h *ScheduleAdminHandler) Trigger() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" {
			httpshared.JSONError(w, apperror.Validation("schedule name is required"))
			return
		}

		if err := h.svc.Trigger(r.Context(), name); err != nil {
			h.audit(r, adminapp.AuditScheduleTrigger, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, adminapp.AuditScheduleTrigger, name, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"status": "triggered"})
	}
}

// Enable handles POST /api/v1/admin/schedules/{name}/enable.
func (h *ScheduleAdminHandler) Enable() http.HandlerFunc {
	return h.setEnabled(true, adminapp.AuditScheduleEnable)
}

// Disable handles POST /api/v1/admin/schedules/{name}/disable.
func (h *ScheduleAdminHandler) Disable() http.HandlerFunc {
	return h.setEnabled(false, adminapp.AuditScheduleDisable)
}

func (h *ScheduleAdminHandler) setEnabled(enabled bool, action adminapp.AuditAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" {
			httpshared.JSONError(w, apperror.Validation("schedule name is required"))
			return
		}

		if err := h.svc.SetEnabled(r.Context(), name, enabled); err != nil {
			h.audit(r, action, name, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, action, name, map[string]interface{}{"enabled": enabled}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"enabled": enabled})
	}
}

func toScheduleResponses(entries []domainscheduler.CatalogEntry) []map[string]interface{} {
	out := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		nextRun := ""
		if !e.NextRun.IsZero() {
			nextRun = e.NextRun.UTC().Format(time.RFC3339)
		}
		out[i] = map[string]interface{}{
			"name":     e.Name,
			"spec":     e.Spec,
			"next_run": nextRun,
			"enabled":  e.Enabled,
		}
	}
	return out
}
