package search

import (
	"context"
	"fmt"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// reindexBatchSize mirrors the batch size the inline CLI loop used before
// this PR (cmd/api's old runSearchReindex) — same behavior, now running
// inside a job handler instead of the CLI process.
const reindexBatchSize = 100

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

	if err := h.runs.Finish(ctx, runID, domainsearch.RunStatusCompleted, ""); err != nil {
		h.log.Error("search.reindex.finish_failed", err, map[string]interface{}{"run_id": runID})
	}
	h.log.Info("search.reindex.complete", map[string]interface{}{
		"run_id":  runID,
		"indexed": processed,
	})
	return nil
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
	if err := h.runs.Finish(ctx, runID, domainsearch.RunStatusFailed, cause.Error()); err != nil {
		h.log.Error("search.reindex.finish_failed", err, map[string]interface{}{"run_id": runID})
	}
}
