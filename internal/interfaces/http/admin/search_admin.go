package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// singleItemIndexTimeout bounds triggerSingleProduct's/triggerSingleCategory's
// DB lookup and engine index call — the whole point of the synchronous
// path is that "reindex this one now" feels instant, not that it blocks
// the request (and holds a DB connection) for however long a slow query
// or a stalled search engine takes. r.Context() alone doesn't provide
// this: it's only cancelled by client disconnect or server shutdown,
// neither of which bounds a slow-but-still-connected caller to "a few
// seconds."
const singleItemIndexTimeout = 5 * time.Second

// SearchAdminHandler serves the admin reindex-trigger, progress, and
// history-listing endpoints (PR-1035, PR-1038) on top of
// application/search's ReindexService (bulk, queued path) and
// SearchEngine (synchronous single-item path).
type SearchAdminHandler struct {
	reindex    *searchApp.ReindexService
	runs       domainsearch.RunStore
	products   domainsearch.ProductSource
	categories domainsearch.CategorySource
	engine     domainsearch.SearchEngine
	auditor    *adminapp.Auditor
}

// NewSearchAdminHandler creates a SearchAdminHandler.
func NewSearchAdminHandler(reindex *searchApp.ReindexService, runs domainsearch.RunStore, products domainsearch.ProductSource, categories domainsearch.CategorySource, engine domainsearch.SearchEngine, auditor *adminapp.Auditor) *SearchAdminHandler {
	if reindex == nil {
		panic("http: search admin reindex service must not be nil")
	}
	if runs == nil {
		panic("http: search admin run store must not be nil")
	}
	if products == nil {
		panic("http: search admin product source must not be nil")
	}
	if categories == nil {
		panic("http: search admin category source must not be nil")
	}
	if engine == nil {
		panic("http: search admin engine must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &SearchAdminHandler{reindex: reindex, runs: runs, products: products, categories: categories, engine: engine, auditor: auditor}
}

// audit records one audit entry. ctx governs LogAction's own synchronous
// persistence call (Auditor.persistEntry's DB insert) — deliberately a
// separate parameter from r, not always r.Context(): triggerSingleProduct
// passes its own singleProductIndexTimeout-bounded context so a slow
// audit-log write can't hold that path's "supposedly bounded" request open
// past its deadline the same way an unbounded ListByIDs/IndexProduct call
// used to (see that function's own doc comment). Every other call site
// still passes r.Context(), unchanged, since only the synchronous path
// makes a bounded-latency promise.
func (h *SearchAdminHandler) audit(ctx context.Context, r *http.Request, action adminapp.AuditAction, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(ctx, adminapp.AuditEntry{
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
// everything else under scope="products" (multiple IDs), all of
// scope="categories" (any ID count, including exactly one), scope=
// "since", and scope="all" is enqueued via ReindexService.Trigger and
// returns 202 with a run ID to poll.
//
// scope="categories" resolves to that category's *member products*
// (ProductSource.ProductIDsByCategory, then IndexProduct for each) — it
// has meant this since PR-1034, well before category documents
// (SearchEngine.IndexCategory, PR-1037) existed at all, and existing
// callers depend on that meaning regardless of how many IDs they send.
// PR-1038 briefly made a single category ID take a synchronous path that
// called IndexCategory instead — updating only the category's own
// document (name/slug/product_count) while silently leaving its member
// products' search entries unrefreshed, changing scope="categories"'
// established meaning for exactly the ID-count-1 case. Reverted: a
// category *document*'s own synchronous reindex is scope=
// "category_document" instead, a distinct, additive scope requiring
// exactly one ID (see triggerSingleCategory) — never scope="categories",
// no matter the ID count.
//
// Every rejection below is audited here, via reject — decode/shape
// failures never reach triggerBulk/triggerSingleProduct, which own
// auditing their own (mutually exclusive) success/failure outcomes, so
// this never produces two audit entries for one request.
func (h *SearchAdminHandler) Trigger() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reject := func(err error, details map[string]interface{}) {
			h.audit(r.Context(), r, adminapp.AuditSearchReindexTrigger, "", details, err)
			httpshared.JSONError(w, err)
		}

		var req reindexTriggerRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			reject(apperror.Validation("invalid request body"), map[string]interface{}{})
			return
		}
		if err := requireSingleJSONValue(dec); err != nil {
			reject(apperror.Validation(err.Error()), map[string]interface{}{"scope": req.Scope})
			return
		}

		switch req.Scope {
		case "all":
			h.triggerBulk(w, r, searchApp.ScopeAll{}, map[string]interface{}{"scope": "all"})

		case "products":
			ids, err := cleanIDs(req.IDs)
			if err != nil {
				reject(apperror.Validation(err.Error()), map[string]interface{}{"scope": "products"})
				return
			}
			if len(ids) == 0 {
				reject(apperror.Validation("ids must not be empty for scope=products"), map[string]interface{}{"scope": "products"})
				return
			}
			if len(ids) == 1 {
				if !id.IsValid(ids[0]) {
					reject(apperror.Validation("product id is not a valid UUID"), map[string]interface{}{"scope": "products", "ids": ids})
					return
				}
				h.triggerSingleProduct(w, r, ids[0])
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeProducts{IDs: ids}, map[string]interface{}{"scope": "products", "ids": ids})

		case "categories":
			ids, err := cleanIDs(req.IDs)
			if err != nil {
				reject(apperror.Validation(err.Error()), map[string]interface{}{"scope": "categories"})
				return
			}
			if len(ids) == 0 {
				reject(apperror.Validation("ids must not be empty for scope=categories"), map[string]interface{}{"scope": "categories"})
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeCategories{IDs: ids}, map[string]interface{}{"scope": "categories", "ids": ids})

		case "category_document":
			ids, err := cleanIDs(req.IDs)
			if err != nil {
				reject(apperror.Validation(err.Error()), map[string]interface{}{"scope": "category_document"})
				return
			}
			if len(ids) != 1 {
				reject(apperror.Validation("category_document requires exactly one id"), map[string]interface{}{"scope": "category_document", "ids": ids})
				return
			}
			h.triggerSingleCategory(w, r, ids[0])

		case "since":
			since := strings.TrimSpace(req.Since)
			if since == "" {
				reject(apperror.Validation("since is required for scope=since"), map[string]interface{}{"scope": "since"})
				return
			}
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				reject(apperror.Validation("since must be an RFC3339 timestamp"), map[string]interface{}{"scope": "since", "since": since})
				return
			}
			if t.After(time.Now().UTC()) {
				reject(apperror.Validation("since must not be in the future"), map[string]interface{}{"scope": "since", "since": since})
				return
			}
			h.triggerBulk(w, r, searchApp.ScopeSince{Since: t}, map[string]interface{}{"scope": "since", "since": since})

		default:
			reject(apperror.Validation(`scope must be one of "all", "products", "categories", "category_document", "since"`), map[string]interface{}{"scope": req.Scope})
		}
	}
}

// requireSingleJSONValue reports an error if dec has another JSON value
// queued up after the one already decoded. json.Decoder.Decode only reads
// a single value and silently leaves the rest of the stream unread, so a
// body like `{"scope":"all"}{"scope":"products"}` would otherwise decode
// the first object successfully and go on to trigger a real reindex,
// silently ignoring (and losing any record of) whatever followed it.
func requireSingleJSONValue(dec *json.Decoder) error {
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON value")
	}
	return nil
}

// triggerSingleProduct indexes exactly one product synchronously, skipping
// the queue entirely — see Trigger's doc comment. ctx is bounded by
// singleItemIndexTimeout, not r.Context() directly: this path exists to
// feel instant, and mustn't hold the request (and a DB connection) open
// indefinitely if Postgres or the search engine stalls.
func (h *SearchAdminHandler) triggerSingleProduct(w http.ResponseWriter, r *http.Request, productID string) {
	ctx, cancel := context.WithTimeout(r.Context(), singleItemIndexTimeout)
	defer cancel()

	products, err := h.products.ListByIDs(ctx, []string{productID})
	if err != nil {
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "look up product failed", err))
		return
	}
	if len(products) == 0 {
		err := apperror.NotFound("product not found")
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, err)
		return
	}
	if err := h.engine.IndexProduct(ctx, products[0]); err != nil {
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "index product failed", err))
		return
	}

	h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, productID, map[string]interface{}{"scope": "products", "ids": []string{productID}, "mode": "sync"}, nil)
	httpshared.JSON(w, http.StatusOK, map[string]interface{}{
		"status":     "indexed",
		"product_id": productID,
	})
}

// triggerSingleCategory indexes exactly one category's own search
// document synchronously (SearchEngine.IndexCategory) — name/slug/
// product_count, not its member products, which is what scope=
// "categories" reindexes instead (see Trigger's own doc comment on why
// these are deliberately separate scopes, not the same one). Category
// IDs aren't UUIDs (see cleanIDs' own doc comment), so there's no format
// check before the lookup — a malformed ID here simply won't be found,
// same as a well-formed but nonexistent one.
func (h *SearchAdminHandler) triggerSingleCategory(w http.ResponseWriter, r *http.Request, categoryID string) {
	ctx, cancel := context.WithTimeout(r.Context(), singleItemIndexTimeout)
	defer cancel()

	category, found, err := h.categories.GetByID(ctx, categoryID)
	if err != nil {
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, categoryID, map[string]interface{}{"scope": "category_document", "ids": []string{categoryID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "look up category failed", err))
		return
	}
	if !found {
		err := apperror.NotFound("category not found")
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, categoryID, map[string]interface{}{"scope": "category_document", "ids": []string{categoryID}, "mode": "sync"}, err)
		httpshared.JSONError(w, err)
		return
	}
	if err := h.engine.IndexCategory(ctx, category); err != nil {
		h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, categoryID, map[string]interface{}{"scope": "category_document", "ids": []string{categoryID}, "mode": "sync"}, err)
		httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "index category failed", err))
		return
	}

	h.audit(ctx, r, adminapp.AuditSearchReindexTrigger, categoryID, map[string]interface{}{"scope": "category_document", "ids": []string{categoryID}, "mode": "sync"}, nil)
	httpshared.JSON(w, http.StatusOK, map[string]interface{}{
		"status":      "indexed",
		"category_id": categoryID,
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
		h.audit(r.Context(), r, adminapp.AuditSearchReindexTrigger, "", auditDetails, err)
		httpshared.JSONError(w, err)
		return
	}

	h.audit(r.Context(), r, adminapp.AuditSearchReindexTrigger, runID, auditDetails, nil)
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
			h.audit(r.Context(), r, adminapp.AuditSearchReindexRead, runID, nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get reindex run failed", err))
			return
		}
		if run == nil {
			err := apperror.NotFound("reindex run not found")
			h.audit(r.Context(), r, adminapp.AuditSearchReindexRead, runID, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r.Context(), r, adminapp.AuditSearchReindexRead, runID, nil, nil)
		httpshared.JSON(w, http.StatusOK, toReindexRunResponse(*run))
	}
}

// List handles GET /api/v1/admin/search/reindex — PR-1038's run-history
// read model. Distinct from Get: List returns a page of runs (most
// recently started first, unfiltered by scope/status — the history table
// this backs has no filter UI), Get returns one run's full detail by ID.
func (h *SearchAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		runs, err := h.runs.List(r.Context(), limit, offset)
		if err != nil {
			h.audit(r.Context(), r, adminapp.AuditSearchReindexList, "", nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "list reindex runs failed", err))
			return
		}

		h.audit(r.Context(), r, adminapp.AuditSearchReindexList, "", map[string]interface{}{"count": len(runs)}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"runs": toReindexRunListResponse(runs),
		})
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

func toReindexRunListResponse(runs []domainsearch.Run) []map[string]interface{} {
	out := make([]map[string]interface{}, len(runs))
	for i, run := range runs {
		out[i] = toReindexRunResponse(run)
	}
	return out
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
