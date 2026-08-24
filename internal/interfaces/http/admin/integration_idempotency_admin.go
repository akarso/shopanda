package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	domainintegration "github.com/akarso/shopanda/internal/domain/integration"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// IntegrationIdempotencyAdminHandler serves read-only inbound integration idempotency admin APIs.
type IntegrationIdempotencyAdminHandler struct {
	repo domainintegration.IdempotencyAdminRepository
}

// NewIntegrationIdempotencyAdminHandler creates an IntegrationIdempotencyAdminHandler.
func NewIntegrationIdempotencyAdminHandler(repo domainintegration.IdempotencyAdminRepository) *IntegrationIdempotencyAdminHandler {
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
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		completed, err := parseOptionalBoolQuery(r, "completed")
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		items, err := h.repo.List(r.Context(), domainintegration.IdempotencyListFilter{
			PluginSlug: strings.TrimSpace(r.URL.Query().Get("plugin")),
			Completed:  completed,
			Offset:     offset,
			Limit:      limit,
		})
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		resp := make([]integrationIdempotencyResp, 0, len(items))
		for _, item := range items {
			resp = append(resp, toIntegrationIdempotencyResp(item, false))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
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
			httpshared.JSONError(w, apperror.Validation("plugin and idempotency key are required"))
			return
		}

		record, err := h.repo.Get(r.Context(), pluginSlug, key)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if record == nil {
			httpshared.JSONError(w, apperror.NotFound("idempotency record not found"))
			return
		}

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
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
			httpshared.JSONError(w, apperror.Validation("plugin and idempotency key are required"))
			return
		}

		record, err := h.repo.Get(r.Context(), pluginSlug, key)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if record == nil {
			httpshared.JSONError(w, apperror.NotFound("idempotency record not found"))
			return
		}
		if !record.Completed {
			httpshared.JSONError(w, apperror.Validation("idempotency record is not completed; nothing to replay"))
			return
		}

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"replayed":      true,
			"status_code":   record.StatusCode,
			"response_body": rawJSONOrString(record.ResponseBody),
		})
	}
}

func toIntegrationIdempotencyResp(item domainintegration.IdempotencyAdminRecord, includeBody bool) integrationIdempotencyResp {
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
	if !utf8.Valid(body) {
		return json.RawMessage(`"<non-utf8 body>"`)
	}
	encoded, err := json.Marshal(string(body))
	if err != nil {
		return json.RawMessage(`"<non-utf8 body>"`)
	}
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
