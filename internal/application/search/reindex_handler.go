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
// only in failIfTerminal's terminal branch (job.Attempts >= job.MaxRetries)
// — there is no next job attempt to fall back on there: once this write
// gives up, the run row has nothing left that will ever call Finish for it
// again (the job itself is about to be marked permanently done by the
// queue regardless of whether this write ever succeeds). It's worth
// trying noticeably harder before accepting that outcome. This reduces,
// but cannot fully eliminate, the residual case where RunStore is
// unavailable for the entire terminal window — see failIfTerminal's doc
// comment and RUNBOOK.md's "Search reindex" section for what's left to do
// if it happens anyway.
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
// updating the run row's progress as it goes. Full-scope reindex covers
// exactly what the old inline CLI loop covered — the same products, same
// fields — running through the queue instead of the CLI process
// (partial/scoped reindex is PR-1034). Any single indexing error still
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

	total, err := h.products.CountAll(ctx)
	if err != nil {
		wrapped := fmt.Errorf("count products: %w", err)
		h.failIfTerminal(ctx, runID, job, wrapped)
		return wrapped
	}

	var processed int
	if err := h.runs.UpdateProgress(ctx, runID, total, processed, 0); err != nil {
		h.log.Error("search.reindex.progress_update_failed", err, map[string]interface{}{"run_id": runID})
	}

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

		for _, p := range products {
			if err := h.engine.IndexProduct(ctx, p); err != nil {
				wrapped := fmt.Errorf("index product %s: %w", p.ID, err)
				h.failIfTerminal(ctx, runID, job, wrapped)
				return wrapped
			}
			processed++
		}
		offset += len(products)

		if err := h.runs.UpdateProgress(ctx, runID, total, processed, 0); err != nil {
			h.log.Error("search.reindex.progress_update_failed", err, map[string]interface{}{"run_id": runID})
		}
	}

	if err := h.finishWithRetry(ctx, runID, domainsearch.RunStatusCompleted, "", finishRetries, finishRetryDelay); err != nil {
		// Indexing itself succeeded, but we could not persist that fact —
		// completing the job here regardless would leave the run row
		// stuck at "processing" forever (nothing else will ever call
		// Finish for it once the job is done). Return an error instead:
		// the worker fails/retries the job like any other operation
		// failure, giving this run another attempt at persisting a
		// terminal status (failIfTerminal marks it failed outright once
		// attempts are exhausted, rather than leaving it processing
		// forever either).
		wrapped := fmt.Errorf("finish run as completed: %w", err)
		h.failIfTerminal(ctx, runID, job, wrapped)
		return wrapped
	}
	h.log.Info("search.reindex.complete", map[string]interface{}{
		"run_id":  runID,
		"indexed": processed,
	})
	return nil
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
