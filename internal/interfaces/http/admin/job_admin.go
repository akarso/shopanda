package admin

import (
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// JobAdminHandler serves admin job introspection and lifecycle-correction
// endpoints (list/detail/retry/cancel) on top of the PR-1028 jobs.Service.
type JobAdminHandler struct {
	svc     *jobsApp.Service
	auditor *adminapp.Auditor
}

// NewJobAdminHandler creates a JobAdminHandler.
func NewJobAdminHandler(svc *jobsApp.Service, auditor *adminapp.Auditor) *JobAdminHandler {
	if svc == nil {
		panic("http: job admin service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &JobAdminHandler{svc: svc, auditor: auditor}
}

func (h *JobAdminHandler) audit(r *http.Request, action adminapp.AuditAction, jobID string, details map[string]interface{}, err error) {
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
		ResourceType: "job",
		ResourceID:   jobID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/jobs.
func (h *JobAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		filter := domainjobs.ListFilter{
			Type:   strings.TrimSpace(r.URL.Query().Get("type")),
			Status: domainjobs.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  limit,
			Offset: offset,
		}

		jobList, err := h.svc.List(r.Context(), filter)
		if err != nil {
			h.audit(r, adminapp.AuditJobList, "", nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "list jobs failed", err))
			return
		}

		h.audit(r, adminapp.AuditJobList, "", map[string]interface{}{
			"type":   filter.Type,
			"status": string(filter.Status),
			"count":  len(jobList),
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"jobs": toJobSummaryResponses(jobList),
		})
	}
}

// Get handles GET /api/v1/admin/jobs/{id}.
func (h *JobAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpshared.JSONError(w, apperror.Validation("job id is required"))
			return
		}

		job, err := h.svc.Get(r.Context(), id)
		if err != nil {
			h.audit(r, adminapp.AuditJobRead, id, nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get job failed", err))
			return
		}
		if job == nil {
			err := apperror.NotFound("job not found")
			h.audit(r, adminapp.AuditJobRead, id, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, adminapp.AuditJobRead, id, nil, nil)
		httpshared.JSON(w, http.StatusOK, toJobDetailResponse(*job))
	}
}

// Retry handles POST /api/v1/admin/jobs/{id}/retry.
func (h *JobAdminHandler) Retry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpshared.JSONError(w, apperror.Validation("job id is required"))
			return
		}

		if err := h.svc.Retry(r.Context(), id); err != nil {
			h.audit(r, adminapp.AuditJobRetry, id, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, adminapp.AuditJobRetry, id, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"status": "pending"})
	}
}

// Cancel handles POST /api/v1/admin/jobs/{id}/cancel.
func (h *JobAdminHandler) Cancel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpshared.JSONError(w, apperror.Validation("job id is required"))
			return
		}

		if err := h.svc.Cancel(r.Context(), id); err != nil {
			h.audit(r, adminapp.AuditJobCancel, id, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, adminapp.AuditJobCancel, id, nil, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"status": "cancelled"})
	}
}

func toJobSummaryResponses(summaries []domainjobs.Summary) []map[string]interface{} {
	out := make([]map[string]interface{}, len(summaries))
	for i, s := range summaries {
		out[i] = map[string]interface{}{
			"id":          s.ID,
			"type":        s.Type,
			"status":      string(s.Status),
			"attempts":    s.Attempts,
			"max_retries": s.MaxRetries,
			"run_at":      s.RunAt.UTC().Format(time.RFC3339),
			"created_at":  s.CreatedAt.UTC().Format(time.RFC3339),
			"updated_at":  s.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	return out
}

func toJobDetailResponse(d domainjobs.Detail) map[string]interface{} {
	return map[string]interface{}{
		"id":          d.ID,
		"type":        d.Type,
		"status":      string(d.Status),
		"attempts":    d.Attempts,
		"max_retries": d.MaxRetries,
		"run_at":      d.RunAt.UTC().Format(time.RFC3339),
		"created_at":  d.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  d.UpdatedAt.UTC().Format(time.RFC3339),
		"payload":     d.Payload,
		"last_error":  d.LastError,
	}
}
