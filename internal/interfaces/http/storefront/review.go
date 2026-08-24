package storefront

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// ReviewHandler serves public product review endpoints.
type ReviewHandler struct {
	reviews *reviewsApp.Service
}

// NewReviewHandler creates a ReviewHandler.
func NewReviewHandler(reviews *reviewsApp.Service) *ReviewHandler {
	if reviews == nil {
		panic("http: review service must not be nil")
	}
	return &ReviewHandler{reviews: reviews}
}

// List handles GET /api/v1/products/{id}/reviews.
func (h *ReviewHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := r.PathValue("id")
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if limit <= 0 || limit > 50 {
			limit = 20
		}

		list, err := h.reviews.ListApprovedByProduct(r.Context(), productID, offset, limit)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"reviews": toPublicReviewResponses(list),
			"offset":  offset,
			"limit":   limit,
		})
	}
}

// ReviewAccountHandler serves authenticated customer review endpoints.
type ReviewAccountHandler struct {
	reviews *reviewsApp.Service
}

// NewReviewAccountHandler creates a ReviewAccountHandler.
func NewReviewAccountHandler(reviews *reviewsApp.Service) *ReviewAccountHandler {
	if reviews == nil {
		panic("http: review service must not be nil")
	}
	return &ReviewAccountHandler{reviews: reviews}
}

type submitReviewBody struct {
	Rating int    `json:"rating"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// Submit handles POST /api/v1/products/{id}/reviews.
func (h *ReviewAccountHandler) Submit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		productID := r.PathValue("id")

		var req submitReviewBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		rev, err := h.reviews.Submit(r.Context(), productID, customerID, reviewsApp.SubmitInput{
			Rating: req.Rating,
			Title:  req.Title,
			Body:   req.Body,
		})
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
			"review": ToAdminReviewResponse(rev),
		})
	}
}

type reviewResp struct {
	ID           string `json:"id"`
	ProductID    string `json:"product_id"`
	CustomerID   string `json:"customer_id,omitempty"`
	Rating       int    `json:"rating"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Status       string `json:"status"`
	ReviewerName string `json:"reviewer_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func toPublicReviewResponse(rev *domainReview.Review) reviewResp {
	if rev == nil {
		return reviewResp{}
	}
	return reviewResp{
		ID:           rev.ID,
		ProductID:    rev.ProductID,
		Rating:       rev.Rating,
		Title:        rev.Title,
		Body:         rev.Body,
		Status:       string(rev.Status()),
		ReviewerName: rev.ReviewerName,
		CreatedAt:    rev.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    rev.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func ToAdminReviewResponse(rev *domainReview.Review) reviewResp {
	resp := toPublicReviewResponse(rev)
	resp.CustomerID = rev.CustomerID
	return resp
}

func toPublicReviewResponses(list []domainReview.Review) []reviewResp {
	out := make([]reviewResp, 0, len(list))
	for i := range list {
		out = append(out, toPublicReviewResponse(&list[i]))
	}
	return out
}

func ToAdminReviewResponses(list []domainReview.Review) []reviewResp {
	out := make([]reviewResp, 0, len(list))
	for i := range list {
		out = append(out, ToAdminReviewResponse(&list[i]))
	}
	return out
}

func ParseReviewStatus(raw string) (domainReview.Status, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	status := domainReview.Status(raw)
	if !status.IsValid() {
		return "", apperror.Validation("invalid review status")
	}
	return status, nil
}
