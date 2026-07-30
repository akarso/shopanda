package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type integrationIdempotencyAdminRepo interface {
	List(ctx context.Context, filter postgres.IntegrationIdempotencyListFilter) ([]postgres.IntegrationIdempotencyAdminRecord, error)
	Get(ctx context.Context, pluginSlug, key string) (*postgres.IntegrationIdempotencyAdminRecord, error)
}

// IntegrationIdempotencyAdminHandler serves read-only inbound integration idempotency admin APIs.
type IntegrationIdempotencyAdminHandler struct {
	repo integrationIdempotencyAdminRepo
}

// NewIntegrationIdempotencyAdminHandler creates an IntegrationIdempotencyAdminHandler.
func NewIntegrationIdempotencyAdminHandler(repo integrationIdempotencyAdminRepo) *IntegrationIdempotencyAdminHandler {
	if repo == nil {
		panic("http: integration idempotency repository must not be nil")
	}
	return &IntegrationIdempotencyAdminHandler{repo: repo}
}

type integrationIdempotencyResp struct {
	PluginSlug     string          `json:"plugin_slug"`
	IdempotencyKey string          `json:"idempotency_key"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	RequestHash    string          `json:"request_hash"`
	StatusCode     int             `json:"status_code"`
	ResponseBody   json.RawMessage `json:"response_body,omitempty"`
	Completed      bool            `json:"completed"`
	CanReplay      bool            `json:"can_replay"`
	CreatedAt      string          `json:"created_at"`
	ExpiresAt      string          `json:"expires_at"`
}

// List handles GET /api/v1/admin/integrations/idempotency.
func (h *IntegrationIdempotencyAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		completed, err := parseOptionalBoolQuery(r, "completed")
		if err != nil {
			JSONError(w, err)
			return
		}

		items, err := h.repo.List(r.Context(), postgres.IntegrationIdempotencyListFilter{
			PluginSlug: strings.TrimSpace(r.URL.Query().Get("plugin")),
			Completed:  completed,
			Offset:     offset,
			Limit:      limit,
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		resp := make([]integrationIdempotencyResp, 0, len(items))
		for _, item := range items {
			resp = append(resp, toIntegrationIdempotencyResp(item, false))
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"records": resp,
			"offset":  offset,
			"limit":   limit,
		})
	}
}

// Get handles GET /api/v1/admin/integrations/idempotency/{plugin}/{key}.
func (h *IntegrationIdempotencyAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pluginSlug := strings.TrimSpace(r.PathValue("plugin"))
		key := strings.TrimSpace(r.PathValue("key"))
		if pluginSlug == "" || key == "" {
			JSONError(w, apperror.Validation("plugin and idempotency key are required"))
			return
		}

		record, err := h.repo.Get(r.Context(), pluginSlug, key)
		if err != nil {
			JSONError(w, err)
			return
		}
		if record == nil {
			JSONError(w, apperror.NotFound("idempotency record not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"record": toIntegrationIdempotencyResp(*record, true),
		})
	}
}

// Replay handles POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay.
// Returns the stored integration response (same payload ERP clients receive on idempotent retry).
func (h *IntegrationIdempotencyAdminHandler) Replay() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pluginSlug := strings.TrimSpace(r.PathValue("plugin"))
		key := strings.TrimSpace(r.PathValue("key"))
		if pluginSlug == "" || key == "" {
			JSONError(w, apperror.Validation("plugin and idempotency key are required"))
			return
		}

		record, err := h.repo.Get(r.Context(), pluginSlug, key)
		if err != nil {
			JSONError(w, err)
			return
		}
		if record == nil {
			JSONError(w, apperror.NotFound("idempotency record not found"))
			return
		}
		if !record.Completed {
			JSONError(w, apperror.Validation("idempotency record is not completed; nothing to replay"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"replayed":      true,
			"status_code":   record.StatusCode,
			"response_body": rawJSONOrString(record.ResponseBody),
		})
	}
}

func toIntegrationIdempotencyResp(item postgres.IntegrationIdempotencyAdminRecord, includeBody bool) integrationIdempotencyResp {
	resp := integrationIdempotencyResp{
		PluginSlug:     item.PluginSlug,
		IdempotencyKey: item.IdempotencyKey,
		Method:         item.Method,
		Path:           item.Path,
		RequestHash:    item.RequestHash,
		StatusCode:     item.StatusCode,
		Completed:      item.Completed,
		CanReplay:      item.Completed,
		CreatedAt:      item.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:      item.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if includeBody && len(item.ResponseBody) > 0 {
		resp.ResponseBody = rawJSONOrString(item.ResponseBody)
	}
	return resp
}

func rawJSONOrString(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	encoded, _ := json.Marshal(string(body))
	return encoded
}

func parseOptionalBoolQuery(r *http.Request, name string) (*bool, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return nil, nil
	}
	switch v {
	case "true", "1":
		b := true
		return &b, nil
	case "false", "0":
		b := false
		return &b, nil
	default:
		return nil, apperror.Validation(name + " must be true or false")
	}
}
