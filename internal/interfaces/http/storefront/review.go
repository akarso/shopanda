package storefront

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// ReviewHandler serves public product review endpoints.
type ReviewHandler struct {
	reviews         *reviewsApp.Service
	products        catalog.ProductRepository
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
}

// NewReviewHandler creates a ReviewHandler.
func NewReviewHandler(reviews *reviewsApp.Service) *ReviewHandler {
	if reviews == nil {
		panic("http: review service must not be nil")
	}
	return &ReviewHandler{reviews: reviews}
}

// WithVisibilityGate checks VisibleIndividually on the parent product (PR-1057).
func (h *ReviewHandler) WithVisibilityGate(products catalog.ProductRepository, facts catalog.VisibilityFactsSource) *ReviewHandler {
	h.products = products
	h.visibilityFacts = facts
	return h
}

// List handles GET /api/v1/products/{id}/reviews.
func (h *ReviewHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := r.PathValue("id")
		if !h.parentIndividuallyVisible(w, r, productID) {
			return
		}
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

func (h *ReviewHandler) parentIndividuallyVisible(w http.ResponseWriter, r *http.Request, productID string) bool {
	return reviewParentIndividuallyVisible(w, r, h.products, h.visibilityFacts, h.visibilityOpts, productID)
}

func reviewParentIndividuallyVisible(
	w http.ResponseWriter,
	r *http.Request,
	products catalog.ProductRepository,
	facts catalog.VisibilityFactsSource,
	opts catalog.VisibilityOptions,
	productID string,
) bool {
	if products == nil || facts == nil {
		return true
	}
	p, err := products.FindByID(r.Context(), productID)
	if err != nil {
		httpshared.JSONError(w, err)
		return false
	}
	if p == nil {
		httpshared.JSONError(w, apperror.NotFound("product not found"))
		return false
	}
	ok, err := catalog.AllowWithFacts(r.Context(), p, catalog.VisibilitySurfaceIndividually, facts, opts)
	if err != nil {
		httpshared.JSONError(w, err)
		return false
	}
	if !ok {
		httpshared.JSONError(w, apperror.NotFound("product not found"))
		return false
	}
	return true
}

// ReviewAccountHandler serves authenticated customer review endpoints.
type ReviewAccountHandler struct {
	reviews         *reviewsApp.Service
	products        catalog.ProductRepository
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
}

// NewReviewAccountHandler creates a ReviewAccountHandler.
func NewReviewAccountHandler(reviews *reviewsApp.Service) *ReviewAccountHandler {
	if reviews == nil {
		panic("http: review service must not be nil")
	}
	return &ReviewAccountHandler{reviews: reviews}
}

// WithVisibilityGate checks VisibleIndividually on the parent product (PR-1057).
func (h *ReviewAccountHandler) WithVisibilityGate(products catalog.ProductRepository, facts catalog.VisibilityFactsSource) *ReviewAccountHandler {
	h.products = products
	h.visibilityFacts = facts
	return h
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
		if !reviewParentIndividuallyVisible(w, r, h.products, h.visibilityFacts, h.visibilityOpts, productID) {
			return
		}

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
