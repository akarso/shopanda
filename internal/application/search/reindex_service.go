package search

import (
	"context"
	"fmt"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/id"
)

// JobType is the job.Type value ReindexHandler registers for and
// ReindexService.Trigger enqueues — the queue's dispatch key connecting
// the two halves of this PR.
const JobType = "search.reindex"

// reindexScopeAll/reindexScopeProducts are the only two domainsearch.Run
// "scope" values Trigger ever actually enqueues a job for. A resolved
// ScopeProducts, ScopeCategories, or ScopeSince all collapse to
// reindexScopeProducts once resolved to concrete IDs — ReindexHandler only
// ever needs to know "scan everything" vs. "scan exactly these IDs";
// which *kind* of partial scope was originally requested is preserved in
// the run's ScopeParams for the audit trail (PR-1034's "visible, not
// silent" substitution), not re-derived by the handler.
const (
	reindexScopeAll      = "all"
	reindexScopeProducts = "products"
)

// ReindexService creates a search_index_runs row and enqueues the job that
// will actually perform it, returning the run ID immediately — the caller
// does not wait for the run to finish (that's runSearchReindex's --wait,
// which polls RunStore.Get separately).
type ReindexService struct {
	store             domainsearch.RunStore
	products          domainsearch.ProductSource
	queue             domainjobs.Queue
	log               Logger
	fullScanThreshold float64
}

// NewReindexService creates a ReindexService backed by store, products,
// and queue. fullScanThreshold is the fraction (0.0–1.0) of the catalog a
// resolved partial scope may cover before Trigger substitutes a full scan
// instead — see Trigger's own doc comment; callers construct this from
// config.Config.Search.ReindexFullScanThreshold, trusted verbatim (no
// clamping here — config validation is where an out-of-range value is
// caught, at startup).
func NewReindexService(store domainsearch.RunStore, products domainsearch.ProductSource, queue domainjobs.Queue, log Logger, fullScanThreshold float64) (*ReindexService, error) {
	if store == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil store")
	}
	if products == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil products")
	}
	if queue == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil queue")
	}
	if log == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil log")
	}
	return &ReindexService{store: store, products: products, queue: queue, log: log, fullScanThreshold: fullScanThreshold}, nil
}

// Trigger resolves scope, creates a run row, and enqueues the
// search.reindex job for it, returning the new run's ID.
//
// A partial scope (ScopeProducts, ScopeCategories, ScopeSince) is resolved
// to a concrete product ID list before enqueueing (category/since scopes
// need a catalog query to know which products they even mean); ScopeAll
// needs no resolution. If the resolved ID count exceeds fullScanThreshold
// of the total catalog (ProductSource.CountAll), the job is enqueued as a
// full scan instead of the caller's requested partial scope — see
// PR-1034's "Why": past some fraction, N individual index round-trips
// cost more than one full-scan pass, so a naive partial reindex can be
// slower than reindexing everything. This substitution is visible, not
// silent: the run's ScopeParams still records what was requested; only
// Scope itself (and what the job actually does) changes.
func (s *ReindexService) Trigger(ctx context.Context, scope Scope) (string, error) {
	runScope, runParams, jobProductIDs, err := s.resolveScope(ctx, scope)
	if err != nil {
		return "", fmt.Errorf("search: trigger: %w", err)
	}

	runID := id.New()
	run := domainsearch.Run{
		ID:          runID,
		Scope:       runScope,
		ScopeParams: runParams,
		Status:      domainsearch.RunStatusProcessing,
		StartedAt:   time.Now().UTC(),
	}
	if err := s.store.Create(ctx, run); err != nil {
		return "", fmt.Errorf("search: trigger: create run: %w", err)
	}

	payload := map[string]interface{}{
		"run_id": runID,
		"scope":  runScope,
	}
	if runScope == reindexScopeProducts {
		payload["product_ids"] = jobProductIDs
	}
	job, err := domainjobs.NewJob(id.New(), JobType, payload)
	if err != nil {
		return "", fmt.Errorf("search: trigger: build job: %w", err)
	}
	if err := s.queue.Enqueue(ctx, job); err != nil {
		enqueueErr := fmt.Errorf("search: trigger: enqueue: %w", err)
		// Create already committed the run row as "processing". With no
		// job ever created for it, it would otherwise be stuck there
		// forever — unlike a crashed worker (whose job row is still there
		// for the queue's own retry/fail machinery to act on), there is
		// nothing left to reconcile this run against. Compensate by
		// marking it failed immediately so the failure is visible and
		// actionable (jobs:list / a future admin progress view) instead
		// of silently orphaning the row. Best-effort: if this also fails,
		// the caller still gets enqueueErr and the row is left processing
		// exactly as before this fix — no worse than the bug being fixed.
		if finishErr := s.store.Finish(ctx, runID, domainsearch.RunStatusFailed, enqueueErr.Error()); finishErr != nil {
			s.log.Error("search.reindex.trigger.compensating_finish_failed", finishErr, map[string]interface{}{"run_id": runID})
		}
		return "", enqueueErr
	}
	return runID, nil
}

// resolveScope turns the caller's requested Scope into the scope name and
// params to persist on the run (runParams always records what was
// requested, even when applyThreshold substitutes a full scan — see
// Trigger's doc comment), and the concrete product ID list the job
// payload carries when the resolved scope is "products" (nil for "all").
func (s *ReindexService) resolveScope(ctx context.Context, scope Scope) (runScope string, runParams map[string]interface{}, productIDs []string, err error) {
	switch sc := scope.(type) {
	case ScopeAll:
		return reindexScopeAll, map[string]interface{}{}, nil, nil

	case ScopeProducts:
		if len(sc.IDs) == 0 {
			return "", nil, nil, fmt.Errorf("scope: products: at least one product id is required")
		}
		requested := map[string]interface{}{"requested_scope": "products", "product_ids": sc.IDs}
		return s.applyThreshold(ctx, requested, sc.IDs)

	case ScopeCategories:
		if len(sc.IDs) == 0 {
			return "", nil, nil, fmt.Errorf("scope: categories: at least one category id is required")
		}
		ids, err := s.products.ProductIDsByCategory(ctx, sc.IDs)
		if err != nil {
			return "", nil, nil, fmt.Errorf("resolve category scope: %w", err)
		}
		requested := map[string]interface{}{"requested_scope": "categories", "category_ids": sc.IDs}
		return s.applyThreshold(ctx, requested, ids)

	case ScopeSince:
		if sc.Since.IsZero() {
			return "", nil, nil, fmt.Errorf("scope: since: a non-zero timestamp is required")
		}
		ids, err := s.products.ProductIDsUpdatedSince(ctx, sc.Since)
		if err != nil {
			return "", nil, nil, fmt.Errorf("resolve since scope: %w", err)
		}
		requested := map[string]interface{}{"requested_scope": "since", "since": sc.Since.UTC().Format(time.RFC3339)}
		return s.applyThreshold(ctx, requested, ids)

	default:
		return "", nil, nil, fmt.Errorf("scope: unsupported scope type %T", scope)
	}
}

// applyThreshold decides whether resolvedIDs is small enough to run as a
// scoped "products" reindex, or large enough — per fullScanThreshold —
// that a full scan is cheaper. requestedParams is preserved as the run's
// ScopeParams either way, so a substitution is visible in the run record,
// not silent (see Trigger's doc comment).
func (s *ReindexService) applyThreshold(ctx context.Context, requestedParams map[string]interface{}, resolvedIDs []string) (runScope string, runParams map[string]interface{}, productIDs []string, err error) {
	if len(resolvedIDs) == 0 {
		// Nothing matched — still a valid, if empty, scoped run (the
		// handler's total_count will just be 0); no need to consult the
		// catalog size for a threshold comparison against zero.
		return reindexScopeProducts, requestedParams, resolvedIDs, nil
	}
	total, err := s.products.CountAll(ctx)
	if err != nil {
		return "", nil, nil, fmt.Errorf("count catalog: %w", err)
	}
	if total > 0 && float64(len(resolvedIDs))/float64(total) > s.fullScanThreshold {
		return reindexScopeAll, requestedParams, nil, nil
	}
	return reindexScopeProducts, requestedParams, resolvedIDs, nil
}
