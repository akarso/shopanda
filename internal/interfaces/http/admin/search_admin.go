package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// SearchAdminHandler serves the admin reindex-trigger and progress
// endpoints (PR-1035) on top of application/search's ReindexService (bulk,
// queued path) and SearchEngine (synchronous single-item path).
type SearchAdminHandler struct {
	reindex  *searchApp.ReindexService
	runs     domainsearch.RunStore
	products domainsearch.ProductSource
	engine   domainsearch.SearchEngine
	auditor  *adminapp.Auditor
}

// NewSearchAdminHandler creates a SearchAdminHandler.
func NewSearchAdminHandler(reindex *searchApp.ReindexService, runs domainsearch.RunStore, products domainsearch.ProductSource, engine domainsearch.SearchEngine, auditor *adminapp.Auditor) *SearchAdminHandler {
	if reindex == nil {
		panic("http: search admin reindex service must not be nil")
	}
	if runs == nil {
		panic("http: search admin run store must not be nil")
	}
	if products == nil {
		panic("http: search admin product source must not be nil")
	}
	if engine == nil {
		panic("http: search admin engine must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &SearchAdminHandler{reindex: reindex, runs: runs, products: products, engine: engine, auditor: auditor}
}

func (h *SearchAdminHandler) audit(r *http.Request, action adminapp.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "search_reindex",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// maxReindexRequestIDs bounds the "ids" array on an incoming request,
// rejected before cleanIDs' loop (let alone ReindexService's own
// normalization) ever runs over it — defense in depth against a
// pathologically large array within the global body-size limit
// (cmd/api/wire_routes.go's BodyLimitMiddleware, which already bounds the
// request generally). Mirrors application/search's own
// maxScopedReindexProductIDs (10,000) — that constant is unexported in
// that package and applies later, after resolution/dedup; this is the
// same number applied as an earlier, cheaper input guard.
const maxReindexRequestIDs = 10_000

type reindexTriggerRequest struct {
	Scope string   `json:"scope"`
	IDs   []string `json:"ids"`
	Since string   `json:"since"`
}

// Trigger handles POST /api/v1/admin/search/reindex.
//
// A single product ID under scope="products" is indexed synchronously
// (SearchEngine.IndexProduct) and returns 200 with the result inline —
// everything else (scope="all", multiple IDs, scope="since", or any
// scope="categories" request) is enqueued via ReindexService.Trigger and
// returns 202 with a run ID to poll.
//
// scope="categories" does not get the synchronous single-item path even
// for exactly one ID: that would require SearchEngine.IndexCategory,
// which does not exist yet (PR-1037, still "planned" as of this PR) — see
// PR-1035.md's Round 1 notes. A single category ID is enqueued the same
// as multiple.
func (h *SearchAdminHandler) Trigger() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req reindexTriggerRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		switch req.Scope {
		case "all":
			h.triggerBulk(w, r, searchApp.ScopeAll{}, map[string]interface{}{"scope": "all"})

		case "products":
			ids, err := cleanIDs(req.IDs)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation(err.Error()))
				return
			}
			if len(ids) == 0 {
				httpshared.JSONError(w, apperror.Validation("ids must not be empty for scope=products"))
				return
			}
			if len(ids) == 1 {
				if !id.IsValid(ids[0]) {
					httpshared.JSONError(w, apperror.Validation("product id is not a valid UUID"))
					return
				}
				h.triggerSingleProduct(w, r, ids[0])
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeProducts{IDs: ids}, map[string]interface{}{"scope": "products", "ids": ids})

		case "categories":
			ids, err := cleanIDs(req.IDs)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation(err.Error()))
				return
			}
			if len(ids) == 0 {
				httpshared.JSONError(w, apperror.Validation("ids must not be empty for scope=categories"))
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeCategories{IDs: ids}, map[string]interface{}{"scope": "categories", "ids": ids})

		case "since":
			since := strings.TrimSpace(req.Since)
			if since == "" {
				httpshared.JSONError(w, apperror.Validation("since is required for scope=since"))
				return
			}
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				httpshared.JSONError(w, apperror.Validation("since must be an RFC3339 timestamp"))
				return
			}
			if t.After(time.Now().UTC()) {
				httpshared.JSONError(w, apperror.Validation("since must not be in the future"))
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeSince{Since: t}, map[string]interface{}{"scope": "since", "since": since})

		default:
			httpshared.JSONError(w, apperror.Validation(`scope must be one of "all", "products", "categories", "since"`))
		}
	}
}

// triggerSingleProduct indexes exactly one product synchronously, skipping
// the queue entirely — see Trigger's doc comment.
func (h *SearchAdminHandler) triggerSingleProduct(w http.ResponseWriter, r *http.Request, productID string) {
	products, err := h.products.ListByIDs(r.Context(), []string{productID})
	if err != nil {
		h.audit(r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "look up product failed", err))
		return
	}
	if len(products) == 0 {
		err := apperror.NotFound("product not found")
		h.audit(r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, err)
		return
	}
	if err := h.engine.IndexProduct(r.Context(), products[0]); err != nil {
		h.audit(r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "index product failed", err))
		return
	}

	h.audit(r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, nil)
	httpshared.JSON(w, http.StatusOK, map[string]interface{}{
		"status":     "indexed",
		"product_id": productID,
	})
}

// triggerBulk enqueues scope via ReindexService.Trigger and returns 202
// with the run ID to poll.
//
// Trigger's error can be either a caller mistake (apperror.Validation —
// e.g. a malformed product ID in a multi-ID list, only checked this deep
// since the single-ID synchronous path's own id.IsValid check doesn't run
// here) or a genuine internal failure (DB/queue error); passed straight to
// JSONError, not force-wrapped as apperror.CodeInternal, so JSONError's own
// errors.As unwrapping can tell the two apart — the same pattern
// JobAdminHandler.Retry/Cancel already use for a service that can return
// either kind of error.
func (h *SearchAdminHandler) triggerBulk(w http.ResponseWriter, r *http.Request, scope searchApp.Scope, auditDetails map[string]interface{}) {
	auditDetails["mode"] = "queued"
	runID, err := h.reindex.Trigger(r.Context(), scope)
	if err != nil {
		h.audit(r, adminapp.AuditSearchReindexTrigger, "", auditDetails, err)
		httpshared.JSONError(w, err)
		return
	}

	h.audit(r, adminapp.AuditSearchReindexTrigger, runID, auditDetails, nil)
	httpshared.JSON(w, http.StatusAccepted, map[string]interface{}{"run_id": runID})
}

// Get handles GET /api/v1/admin/search/reindex/{runID}.
func (h *SearchAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(r.PathValue("runID"))
		if runID == "" {
			httpshared.JSONError(w, apperror.Validation("run id is required"))
			return
		}

		run, err := h.runs.Get(r.Context(), runID)
		if err != nil {
			h.audit(r, adminapp.AuditSearchReindexRead, runID, nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get reindex run failed", err))
			return
		}
		if run == nil {
			err := apperror.NotFound("reindex run not found")
			h.audit(r, adminapp.AuditSearchReindexRead, runID, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, adminapp.AuditSearchReindexRead, runID, nil, nil)
		httpshared.JSON(w, http.StatusOK, toReindexRunResponse(*run))
	}
}

// cleanIDs trims and rejects blank entries from a caller-supplied ID list.
// It intentionally doesn't check UUID shape here (categories.id is plain
// TEXT, unlike products.id — the "products" case checks UUID shape
// separately, only on the synchronous single-item path, since the bulk
// path's ReindexService.Trigger already validates via its own
// normalizeProductIDs/normalizeCategoryIDs, PR-1034) or dedupe (same
// reason).
func cleanIDs(ids []string) ([]string, error) {
	if len(ids) > maxReindexRequestIDs {
		return nil, fmt.Errorf("ids must not exceed %d entries", maxReindexRequestIDs)
	}
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		v := strings.TrimSpace(raw)
		if v == "" {
			return nil, fmt.Errorf("id must not be blank")
		}
		out = append(out, v)
	}
	return out, nil
}

func toReindexRunResponse(run domainsearch.Run) map[string]interface{} {
	resp := map[string]interface{}{
		"id":              run.ID,
		"scope":           run.Scope,
		"scope_params":    run.ScopeParams,
		"status":          string(run.Status),
		"total_count":     run.TotalCount,
		"processed_count": run.ProcessedCount,
		"error_count":     run.ErrorCount,
		"started_at":      run.StartedAt.UTC().Format(time.RFC3339),
	}
	if !run.FinishedAt.IsZero() {
		resp["finished_at"] = run.FinishedAt.UTC().Format(time.RFC3339)
	}
	if run.LastError != "" {
		resp["last_error"] = run.LastError
	}
	return resp
}
