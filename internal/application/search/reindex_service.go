package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/apperror"
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

// maxScopedReindexProductIDs bounds a scoped run's resolved product ID
// list independent of fullScanThreshold's percentage check. The job
// payload carrying that list is persisted as JSONB on the jobs row and
// reloaded on every dequeue/retry — a well-under-threshold percentage can
// still be an enormous absolute count on a large catalog (20% of 5
// million products is 1 million UUID strings, tens of MB of JSONB).
// 10,000 IDs is comfortably above any realistic "reindex the batch I just
// changed" partial run — a genuinely bulk change is exactly the case the
// percentage threshold already exists to redirect to a full scan — while
// keeping the payload in the hundreds-of-KB range.
const maxScopedReindexProductIDs = 10_000

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
// of the total catalog (ProductSource.CountAll), or exceeds
// maxScopedReindexProductIDs outright regardless of the catalog's size,
// the job is enqueued as a full scan instead of the caller's requested
// partial scope — see PR-1034's "Why" for the threshold, and
// maxScopedReindexProductIDs' own doc comment for the absolute cap (an
// unbounded ID list would otherwise be serialized whole into the job's
// JSONB payload). This substitution is visible, not silent: the run's
// ScopeParams still records what was requested; only Scope itself (and
// what the job actually does) changes.
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
		ids, err := normalizeProductIDs(sc.IDs)
		if err != nil {
			return "", nil, nil, fmt.Errorf("scope: products: %w", err)
		}
		if len(ids) == 0 {
			return "", nil, nil, fmt.Errorf("scope: products: %w", apperror.Validation("at least one product id is required"))
		}
		requested := map[string]interface{}{"requested_scope": "products", "product_ids": ids}
		return s.applyThreshold(ctx, requested, ids)

	case ScopeCategories:
		catIDs, err := normalizeCategoryIDs(sc.IDs)
		if err != nil {
			return "", nil, nil, fmt.Errorf("scope: categories: %w", err)
		}
		if len(catIDs) == 0 {
			return "", nil, nil, fmt.Errorf("scope: categories: %w", apperror.Validation("at least one category id is required"))
		}
		ids, err := s.products.ProductIDsByCategory(ctx, catIDs)
		if err != nil {
			return "", nil, nil, fmt.Errorf("resolve category scope: %w", err)
		}
		requested := map[string]interface{}{"requested_scope": "categories", "category_ids": catIDs}
		return s.applyThreshold(ctx, requested, ids)

	case ScopeSince:
		if sc.Since.IsZero() {
			return "", nil, nil, fmt.Errorf("scope: since: %w", apperror.Validation("a non-zero timestamp is required"))
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
// scoped "products" reindex, or large enough — per fullScanThreshold or
// maxScopedReindexProductIDs, whichever trips first — that a full scan is
// cheaper (or simply safer to enqueue). requestedParams is preserved as
// the run's ScopeParams either way, so a substitution is visible in the
// run record, not silent (see Trigger's doc comment).
//
// The catalog-size comparison below is two independent reads (resolvedIDs
// was already counted by the caller; CountAll runs here) with no shared
// snapshot — the catalog can change in between, occasionally flipping the
// ratio right at the margin. Accepted, the same way SearchProductSource's
// own scan has no shared snapshot across batches (see its doc comment):
// the cost of being wrong here is bounded either way (a scoped run
// slightly over or under the intended fraction, or a full scan that's
// more thorough than strictly necessary), not a correctness problem.
func (s *ReindexService) applyThreshold(ctx context.Context, requestedParams map[string]interface{}, resolvedIDs []string) (runScope string, runParams map[string]interface{}, productIDs []string, err error) {
	if len(resolvedIDs) == 0 {
		// Nothing matched — still a valid, if empty, scoped run (the
		// handler's total_count will just be 0); no need to consult the
		// catalog size for a threshold comparison against zero.
		//
		// Returns an allocated empty slice, not resolvedIDs itself (which
		// can be a nil []string here — e.g. ProductIDsByCategory found no
		// matches): a nil slice marshals to JSON `null`, and
		// scopedProductIDs' own `.([]interface{})` type assertion on a
		// decoded `null` fails (a JSON null decodes to an untyped nil
		// interface, not an empty slice) — which would make the job fail
		// with "requires a product_ids array" instead of completing a
		// legitimate, if trivial, 0/0 run. `[]string{}` marshals to `[]`
		// and round-trips correctly.
		return reindexScopeProducts, requestedParams, []string{}, nil
	}
	if len(resolvedIDs) > maxScopedReindexProductIDs {
		return reindexScopeAll, requestedParams, nil, nil
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

// normalizeProductIDs validates and deduplicates a caller-supplied product
// ID list (ScopeProducts), preserving first-seen order. Every ID must be
// non-blank and a well-formed UUID — products.id is a UUID column, so a
// malformed ID would otherwise surface late as a Postgres "invalid input
// syntax for type uuid" error only once the job actually runs (and after
// being retried to exhaustion), instead of being rejected synchronously
// here, before any DB/queue work happens. Deduplication matters beyond
// the returned list's own correctness: the count feeds applyThreshold's
// ratio directly and becomes the run's total_count, while ListByIDs' own
// `= ANY($1)` naturally deduplicates on the read side — an
// un-deduplicated input would let total_count permanently exceed the
// processed_count a "completed" run could ever report.
func normalizeProductIDs(ids []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		v := strings.TrimSpace(raw)
		if v == "" {
			return nil, apperror.Validation("product id must not be blank")
		}
		if !id.IsValid(v) {
			return nil, apperror.Validation(fmt.Sprintf("product id %q is not a valid UUID", v))
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// normalizeCategoryIDs validates and deduplicates a caller-supplied
// category ID list (ScopeCategories), preserving first-seen order. Unlike
// normalizeProductIDs, it doesn't require UUID shape — categories.id is a
// plain TEXT column, not UUID-typed — only that no entry is blank.
func normalizeCategoryIDs(ids []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		v := strings.TrimSpace(raw)
		if v == "" {
			return nil, apperror.Validation("category id must not be blank")
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}
