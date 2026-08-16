package http

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
)

// ReviewAdminHandler serves admin product review moderation endpoints.
type ReviewAdminHandler struct {
	reviews *reviewsApp.Service
	auditor *admin.Auditor
}

// NewReviewAdminHandler creates a ReviewAdminHandler.
func NewReviewAdminHandler(reviews *reviewsApp.Service, auditor *admin.Auditor) *ReviewAdminHandler {
	if reviews == nil {
		panic("http: review service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &ReviewAdminHandler{reviews: reviews, auditor: auditor}
}

// List handles GET /api/v1/admin/reviews.
func (h *ReviewAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := ParsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		status, err := parseReviewStatus(r.URL.Query().Get("status"))
		if err != nil {
			JSONError(w, err)
			return
		}

		adminID := adminIDFromRequest(r)
		list, err := h.reviews.List(r.Context(), status, offset, limit)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditReviewList,
				ResourceType: "review",
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditReviewList,
			ResourceType: "review",
			Result:       "success",
			Details: map[string]interface{}{
				"offset": offset,
				"limit":  limit,
				"status": string(status),
				"count":  len(list),
			},
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"reviews": toAdminReviewResponses(list),
			"offset":  offset,
			"limit":   limit,
		})
	}
}

// Get handles GET /api/v1/admin/reviews/{reviewId}.
func (h *ReviewAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		adminID := adminIDFromRequest(r)

		rev, err := h.reviews.Get(r.Context(), reviewID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditReviewRead,
				ResourceType: "review",
				ResourceID:   reviewID,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditReviewRead,
			ResourceType: "review",
			ResourceID:   reviewID,
			Result:       "success",
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"review": toAdminReviewResponse(rev),
		})
	}
}

func (h *ReviewAdminHandler) transition(action admin.AuditAction, fn func(context.Context, string) (*domainReview.Review, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		adminID := adminIDFromRequest(r)

		rev, err := fn(r.Context(), reviewID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       action,
				ResourceType: "review",
				ResourceID:   reviewID,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       action,
			ResourceType: "review",
			ResourceID:   reviewID,
			Result:       "success",
			Details: map[string]interface{}{
				"status": string(rev.Status()),
			},
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"review": toAdminReviewResponse(rev),
		})
	}
}

// Approve handles POST /api/v1/admin/reviews/{reviewId}/approve.
func (h *ReviewAdminHandler) Approve() http.HandlerFunc {
	return h.transition(admin.AuditReviewApprove, h.reviews.Approve)
}

// Reject handles POST /api/v1/admin/reviews/{reviewId}/reject.
func (h *ReviewAdminHandler) Reject() http.HandlerFunc {
	return h.transition(admin.AuditReviewReject, h.reviews.Reject)
}
