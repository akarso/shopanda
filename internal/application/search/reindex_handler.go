package search

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// reindexBatchSize mirrors the batch size the inline CLI loop used before
// this PR (cmd/api's old runSearchReindex) — same behavior, now running
// inside a job handler instead of the CLI process.
const reindexBatchSize = 100

// finishRetries/finishRetryDelay bound a small in-process retry for a
// Finish call that itself fails to persist — see finishWithRetry. A
// transient DB blip on the one write that actually finalizes a run's
// status is worth a few quick retries: on the completed path a failure
// still gets another chance via the job's own retry (failIfTerminal
// leaves the run "processing" for the next attempt), so this budget only
// needs to cover a brief blip, not a real outage.
//
// finishTerminalRetries/finishTerminalRetryDelay is the larger budget used
// on a job's last allowed attempt (job.Attempts >= job.MaxRetries), by
// both failIfTerminal (a genuine indexing/count/list failure) and
// retryCompletedOnTerminalAttempt (persisting a successful reindex's
// "completed" status) — there is no next job attempt to fall back on
// there: once this write gives up, the run row has nothing left that will
// ever call Finish for it again (the job itself is about to be marked
// permanently done by the queue regardless of whether this write ever
// succeeds). It's worth trying noticeably harder before accepting that
// outcome. This reduces, but cannot fully eliminate, the residual case
// where RunStore is unavailable for the entire terminal window — see
// those two methods' doc comments and RUNBOOK.md's "Search reindex"
// section for what's left to do if it happens anyway.
const (
	finishRetries    = 3
	finishRetryDelay = 50 * time.Millisecond

	finishTerminalRetries    = 5
	finishTerminalRetryDelay = 200 * time.Millisecond
)

// Logger is the logging interface used by ReindexHandler.
type Logger interface {
	Info(event string, ctx map[string]interface{})
	Error(event string, err error, ctx map[string]interface{})
}

// ReindexHandler is the jobs.Handler for JobType ("search.reindex"): scans
// ProductSource in batches and indexes every product via SearchEngine,
// updating the run row's progress as it goes. A full-scope run covers
// exactly what the old inline CLI loop covered — the same products, same
// fields — running through the queue instead of the CLI process. A scoped
// run (PR-1034 — explicit product/category IDs, or "changed since",
// resolved by ReindexService.Trigger into a concrete product ID list
// carried on the job payload) scans exactly that list instead of the
// whole table — see scopedProductIDs. Any single indexing error still
// stops the current attempt (no skip-and-continue), but per-attempt
// retries mean the run row is only marked failed once the job has
// exhausted its retries — see failIfTerminal.
type ReindexHandler struct {
	runs     domainsearch.RunStore
	products domainsearch.ProductSource
	engine   domainsearch.SearchEngine
	log      Logger
}

// NewReindexHandler creates a ReindexHandler.
func NewReindexHandler(runs domainsearch.RunStore, products domainsearch.ProductSource, engine domainsearch.SearchEngine, log Logger) *ReindexHandler {
	return &ReindexHandler{runs: runs, products: products, engine: engine, log: log}
}

// Type implements jobs.Handler.
func (h *ReindexHandler) Type() string { return JobType }

// Handle implements jobs.Handler.
func (h *ReindexHandler) Handle(ctx context.Context, job domainjobs.Job) error {
	runID, _ := job.Payload["run_id"].(string)
	if runID == "" {
		return fmt.Errorf("search.reindex: job %s: missing run_id in payload", job.ID)
	}

	// productIDs is nil for a full-scan run (scope "all", or an unset/
	// legacy payload — see scopedProductIDs) and non-nil (possibly empty)
	// for a scoped one (PR-1034): ReindexService.Trigger already resolved
	// exactly which products to cover, so the handler scans that fixed
	// list instead of the whole table.
	productIDs, err := scopedProductIDs(job)
	if err != nil {
		wrapped := fmt.Errorf("search.reindex: job %s: %w", job.ID, err)
		h.failIfTerminal(ctx, runID, job, wrapped)
		return wrapped
	}

	var total int
	if productIDs != nil {
		total = len(productIDs)
	} else {
		total, err = h.products.CountAll(ctx)
		if err != nil {
			wrapped := fmt.Errorf("count products: %w", err)
			h.failIfTerminal(ctx, runID, job, wrapped)
			return wrapped
		}
	}

	var processed int
	if err := h.runs.UpdateProgress(ctx, runID, total, processed, 0); err != nil {
		h.log.Error("search.reindex.progress_update_failed", err, map[string]interface{}{"run_id": runID})
	}

	indexBatch := func(products []domainsearch.Product) error {
		for _, p := range products {
			if err := h.engine.IndexProduct(ctx, p); err != nil {
				return fmt.Errorf("index product %s: %w", p.ID, err)
			}
			processed++
		}
		if err := h.runs.UpdateProgress(ctx, runID, total, processed, 0); err != nil {
			h.log.Error("search.reindex.progress_update_failed", err, map[string]interface{}{"run_id": runID})
		}
		return nil
	}

	if productIDs != nil {
		for offset := 0; offset < len(productIDs); offset += reindexBatchSize {
			end := offset + reindexBatchSize
			if end > len(productIDs) {
				end = len(productIDs)
			}
			products, err := h.products.ListByIDs(ctx, productIDs[offset:end])
			if err != nil {
				wrapped := fmt.Errorf("list products by id (offset=%d): %w", offset, err)
				h.failIfTerminal(ctx, runID, job, wrapped)
				return wrapped
			}
			if err := indexBatch(products); err != nil {
				h.failIfTerminal(ctx, runID, job, err)
				return err
			}
		}
	} else {
		var offset int
		for {
			products, err := h.products.ListAll(ctx, offset, reindexBatchSize)
			if err != nil {
				wrapped := fmt.Errorf("list products (offset=%d): %w", offset, err)
				h.failIfTerminal(ctx, runID, job, wrapped)
				return wrapped
			}
			if len(products) == 0 {
				break
			}
			if err := indexBatch(products); err != nil {
				h.failIfTerminal(ctx, runID, job, err)
				return err
			}
			offset += len(products)
		}
	}

	if err := h.finishWithRetry(ctx, runID, domainsearch.RunStatusCompleted, "", finishRetries, finishRetryDelay); err != nil {
		// Indexing itself succeeded, but we could not persist that fact —
		// completing the job here regardless would leave the run row
		// stuck at "processing" forever (nothing else will ever call
		// Finish for it once the job is done). retryCompletedOnTerminalAttempt
		// gives this one more, longer-budget attempt to persist COMPLETED
		// (not FAILED — see its own doc comment for why that distinction
		// matters) before giving up.
		if err := h.retryCompletedOnTerminalAttempt(ctx, runID, job, err); err != nil {
			return err
		}
	}
	h.log.Info("search.reindex.complete", map[string]interface{}{
		"run_id":  runID,
		"indexed": processed,
	})
	return nil
}

// scopedProductIDs reads the job's scope from its payload and, for a
// "products" scope, the concrete product ID list ReindexService.Trigger
// already resolved (see its own doc comment on why resolution happens
// once, at trigger time — re-resolving a category/since scope here could
// give a different answer than what Trigger's full-scan-threshold check
// actually decided against). Returns (nil, nil) for a full-scan job:
// scope "all", or an unset/wrong-type scope field (every payload built
// before PR-1034 shipped, and any hand-constructed job with no "scope"
// key) — nil is the sentinel Handle uses to pick the ListAll loop instead
// of the ListByIDs one, so this keeps existing full-scan jobs behaving
// exactly as before.
func scopedProductIDs(job domainjobs.Job) ([]string, error) {
	scope, _ := job.Payload["scope"].(string)
	if scope != reindexScopeProducts {
		return nil, nil
	}
	raw, ok := job.Payload["product_ids"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("scope %q requires a \"product_ids\" array in the job payload", scope)
	}
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("scope %q: non-string entry in \"product_ids\"", scope)
		}
		ids = append(ids, s)
	}
	return ids, nil
}

// finishWithRetry calls RunStore.Finish, retrying up to retries times
// (delay apart) on failure — see finishRetries/finishRetryDelay and
// finishTerminalRetries/finishTerminalRetryDelay for the two budgets
// callers pass. Returns the last error if every attempt fails.
//
// domainsearch.ErrRunNotProcessing is treated as success, not a failure to
// retry: Finish is a conditional update guarded on the run still being
// "processing" (see its doc comment), so this specific error means some
// other caller — most likely the reconciliation sweep (PR-1048) — already
// finished the run while this handler was still working. Retrying
// wouldn't change that outcome, and from this handler's own perspective
// the run already has a terminal status, which is the invariant it cares
// about; it's simply not necessarily the status *this* attempt wanted to
// set.
func (h *ReindexHandler) finishWithRetry(ctx context.Context, runID string, status domainsearch.RunStatus, lastErr string, retries int, delay time.Duration) error {
	var err error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err = h.runs.Finish(ctx, runID, status, lastErr)
		if err == nil {
			return nil
		}
		if errors.Is(err, domainsearch.ErrRunNotProcessing) {
			h.log.Info("search.reindex.finish_skipped_already_terminal", map[string]interface{}{
				"run_id":           runID,
				"attempted_status": string(status),
			})
			return nil
		}
	}
	return err
}

// failIfTerminal marks the run failed only when job is on its last allowed
// attempt (job.Attempts >= job.MaxRetries — the same condition
// jobs.Queue's Fail implementation uses to decide a permanent failure vs.
// a requeue-with-delay, see postgres/job_queue.go). The worker retries a
// returned error up to MaxRetries times with backoff; marking the run
// failed on every attempt (including a transient first failure that the
// next retry goes on to fix) would report a false failure to --wait/any
// progress view while the job is still retrying, and a later successful
// attempt would silently flip the same row back to completed — the
// eager version of this method did exactly that. A non-terminal failure
// is only logged here; the run stays "processing" for the next attempt
// to either complete or, on the final attempt, fail for real.
//
// Only for a genuine failure (CountAll/ListAll/IndexProduct erroring) —
// never call this for a Finish(completed) failure, where the reindex
// itself actually succeeded; see retryCompletedOnTerminalAttempt for that
// case, which deliberately does not reuse this method (marking that run
// "failed" would misreport a real success).
//
// Residual gap on the terminal branch: if finishWithRetry's larger
// finishTerminalRetries/finishTerminalRetryDelay budget is still
// exhausted (RunStore unavailable for that whole window), the job is
// about to be marked permanently failed by the queue regardless, and
// nothing will ever call Finish for this run again from inside this
// method — it's left "processing" with no automatic path back to a
// terminal status via this code path. This is the same class of gap a
// worker process crashing mid-run leaves too (see RUNBOOK.md's "Search
// reindex" section). PR-1048's search.reindex.reconcile sweep (and its
// search:reindex-runs:reconcile manual CLI fallback) is the actual fix
// for both: it periodically checks a run stuck "processing" against its
// job's real outcome and corrects it — not something built inline in
// this already-best-effort method.
func (h *ReindexHandler) failIfTerminal(ctx context.Context, runID string, job domainjobs.Job, cause error) {
	if job.Attempts < job.MaxRetries {
		h.log.Info("search.reindex.attempt_failed_will_retry", map[string]interface{}{
			"run_id":      runID,
			"job_id":      job.ID,
			"attempt":     job.Attempts,
			"max_retries": job.MaxRetries,
			"error":       cause.Error(),
		})
		return
	}
	if err := h.finishWithRetry(ctx, runID, domainsearch.RunStatusFailed, cause.Error(), finishTerminalRetries, finishTerminalRetryDelay); err != nil {
		h.log.Error("search.reindex.finish_failed", err, map[string]interface{}{"run_id": runID})
	}
}

// retryCompletedOnTerminalAttempt handles the one case failIfTerminal must
// NOT be used for: the reindex itself succeeded, but persisting
// RunStatusCompleted failed within the short finishRetries budget.
// Marking the run "failed" here (as failIfTerminal would) would misreport
// a genuinely successful reindex — and since finishTerminalRetries'
// budget is strictly *longer* than finishRetries', a transient outage
// that recovers between the two budgets would make exactly that "failed"
// write succeed, permanently mislabeling a fully completed reindex as
// failed and making `--wait` exit non-zero over a correctly rebuilt
// index.
//
// On a non-terminal attempt (more job retries left), this just logs and
// returns the original error, the same as failIfTerminal's own
// non-terminal branch — the next attempt re-scans from scratch
// (idempotent) and tries to persist "completed" again.
//
// On the last allowed attempt, it retries persisting COMPLETED — not
// FAILED — with the longer finishTerminalRetries/finishTerminalRetryDelay
// budget, since COMPLETED is the status actually worth trying hardest for
// here: the work is genuinely done. Returns nil if COMPLETED is
// ultimately persisted (even only on this extra attempt), so the caller
// reports success. If this extra attempt also fails, returns the
// original error and leaves the run "processing" — the same honest
// "nothing could be persisted" outcome failIfTerminal's own residual gap
// documents, and a candidate for PR-1048's reconciliation sweep to
// eventually pick up (via the job's own outcome, once the queue marks it
// failed).
func (h *ReindexHandler) retryCompletedOnTerminalAttempt(ctx context.Context, runID string, job domainjobs.Job, firstErr error) error {
	wrapped := fmt.Errorf("finish run as completed: %w", firstErr)
	if job.Attempts < job.MaxRetries {
		h.log.Info("search.reindex.attempt_failed_will_retry", map[string]interface{}{
			"run_id":      runID,
			"job_id":      job.ID,
			"attempt":     job.Attempts,
			"max_retries": job.MaxRetries,
			"error":       wrapped.Error(),
		})
		return wrapped
	}
	if err := h.finishWithRetry(ctx, runID, domainsearch.RunStatusCompleted, "", finishTerminalRetries, finishTerminalRetryDelay); err != nil {
		h.log.Error("search.reindex.finish_failed", err, map[string]interface{}{"run_id": runID})
		return wrapped
	}
	return nil
}
