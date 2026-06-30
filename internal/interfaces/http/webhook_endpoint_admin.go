package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// WebhookEndpointAdminHandler serves merchant outbound webhook configuration.
type WebhookEndpointAdminHandler struct {
	service *webhookApp.Service
}

// NewWebhookEndpointAdminHandler creates a WebhookEndpointAdminHandler.
func NewWebhookEndpointAdminHandler(service *webhookApp.Service) *WebhookEndpointAdminHandler {
	if service == nil {
		panic("http: webhook service must not be nil")
	}
	return &WebhookEndpointAdminHandler{service: service}
}

type webhookEndpointResponse struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	Events       []string `json:"events"`
	Active       bool     `json:"active"`
	Description  string   `json:"description,omitempty"`
	SecretPrefix string   `json:"secret_prefix,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type createWebhookEndpointRequest struct {
	URL         string   `json:"url"`
	Events      []string `json:"events"`
	Active      *bool    `json:"active"`
	Description string   `json:"description"`
}

type updateWebhookEndpointRequest struct {
	URL          string   `json:"url"`
	Events       []string `json:"events"`
	Active       *bool    `json:"active"`
	Description  string   `json:"description"`
	RotateSecret *bool    `json:"rotate_secret"`
}

// Catalog handles GET /api/v1/admin/webhooks/events.
func (h *WebhookEndpointAdminHandler) Catalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]interface{}{
			"events": domainwebhook.SupportedEvents,
		})
	}
}

// List handles GET /api/v1/admin/webhooks.
func (h *WebhookEndpointAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints, err := h.service.List(r.Context())
		if err != nil {
			JSONError(w, err)
			return
		}
		items := make([]webhookEndpointResponse, 0, len(endpoints))
		for _, ep := range endpoints {
			items = append(items, toWebhookEndpointResponse(ep))
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"endpoints": items,
		})
	}
}

// Get handles GET /api/v1/admin/webhooks/{id}.
func (h *WebhookEndpointAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointID := strings.TrimSpace(r.PathValue("id"))
		if endpointID == "" {
			JSONError(w, apperror.Validation("endpoint id is required"))
			return
		}
		ep, err := h.service.Get(r.Context(), endpointID)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"endpoint": toWebhookEndpointResponse(ep),
		})
	}
}

// Create handles POST /api/v1/admin/webhooks.
func (h *WebhookEndpointAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createWebhookEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		active := true
		if req.Active != nil {
			active = *req.Active
		}
		result, err := h.service.Create(r.Context(), webhookApp.CreateInput{
			URL:         req.URL,
			Events:      req.Events,
			Active:      active,
			Description: req.Description,
		})
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusCreated, map[string]interface{}{
			"endpoint": toWebhookEndpointResponse(result.Endpoint),
			"secret":   result.Secret,
		})
	}
}

// Update handles PUT /api/v1/admin/webhooks/{id}.
func (h *WebhookEndpointAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointID := strings.TrimSpace(r.PathValue("id"))
		if endpointID == "" {
			JSONError(w, apperror.Validation("endpoint id is required"))
			return
		}
		var req updateWebhookEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		rotate := req.RotateSecret != nil && *req.RotateSecret
		ep, secret, err := h.service.Update(r.Context(), webhookApp.UpdateInput{
			ID:           endpointID,
			URL:          req.URL,
			Events:       req.Events,
			Active:       req.Active,
			Description:  req.Description,
			RotateSecret: rotate,
		})
		if err != nil {
			JSONError(w, err)
			return
		}
		resp := map[string]interface{}{
			"endpoint": toWebhookEndpointResponse(ep),
		}
		if secret != "" {
			resp["secret"] = secret
		}
		JSON(w, http.StatusOK, resp)
	}
}

// Delete handles DELETE /api/v1/admin/webhooks/{id}.
func (h *WebhookEndpointAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpointID := strings.TrimSpace(r.PathValue("id"))
		if endpointID == "" {
			JSONError(w, apperror.Validation("endpoint id is required"))
			return
		}
		if err := h.service.Delete(r.Context(), endpointID); err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}

func toWebhookEndpointResponse(ep webhookApp.EndpointView) webhookEndpointResponse {
	return webhookEndpointResponse{
		ID:           ep.ID,
		URL:          ep.URL,
		Events:       ep.Events,
		Active:       ep.Active,
		Description:  ep.Description,
		SecretPrefix: ep.SecretPrefix,
		CreatedAt:    ep.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    ep.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
