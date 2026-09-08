package search

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ReconcileJobType is the job.Type value ReconcileHandler registers for.
const ReconcileJobType = "search.reindex.reconcile"

// staleAfter is how long a run may sit in "processing" before the
// reconciliation sweep treats it as a candidate worth checking against
// its underlying job. Comfortably above how long even a large full-catalog
// reindex should normally take (reindexBatchSize=100 products/batch), and
// above the job queue's own retry window (DefaultMaxRetries=3, backoff
// capped at backoffMax=5 minutes — a job that will eventually succeed or
// exhaust its retries does so within a handful of minutes of its first
// attempt). ReindexHandler indexes one product at a time with no
// concurrency, so an unusually slow SearchEngine could in principle make a
// legitimate run exceed this — functionally harmless (its job still reads
// as non-terminal, so the sweep just logs job_not_terminal and leaves it
// alone), just a reminder this is a heuristic, not a hard proof of being
// stuck.
const staleAfter = 2 * time.Hour

// reconcileBatchLimit bounds a single sweep invocation to at most this
// many stale runs — a mass-orphan event (e.g. a bad deploy crashing many
// workers at once) must not make one tick process an unbounded number of
// rows. A run left over a full batch stays "processing" and so stays a
// match for the next tick, draining the backlog across however many ticks
// it takes — the same pattern reservation_expiry_handler.go's own batching
// uses.
const reconcileBatchLimit = 500

// reconcileSweepTimeout bounds a single sweep invocation's total wall
// time, well under the 30-minute cron interval so a normal (small) sweep
// never runs anywhere near it, but long enough to make real progress
// across reconcileBatchLimit runs on a large backlog. Mirrors
// reservation_expiry_handler.go's sweepTimeout.
const reconcileSweepTimeout = 5 * time.Minute

// ReconcileHandler processes search.reindex.reconcile jobs: it sweeps
// search_index_runs for rows stuck "processing" past staleAfter and, for
// each, looks up the corresponding search.reindex job by run_id (the
// payload key ReindexService.Trigger sets). Two gaps otherwise leave a run
// row "processing" forever with nothing left to ever change it (see
// reindex_handler.go's own doc comments and RUNBOOK.md's "Search reindex"
// section): the worker process crashing before Finish is ever called, or
// every retry of the terminal Finish write itself failing on the job's
// last allowed attempt. This handler closes the case that's safe to
// correct automatically — the underlying job has already reached a real
// terminal status (done/failed/cancelled), so nothing will ever touch the
// run row again either.
//
// It deliberately does NOT touch a run whose job is still
// "pending"/"processing" (or entirely missing but recently created) —
// from job status alone there's no way to tell a genuinely orphaned job
// (crashed worker, no reaper ever requeues it — see RUNBOOK.md) apart from
// one that's still legitimately running or retrying. That ambiguous case
// is left for an operator to judge, via `search:reindex-runs:reconcile`
// (ReconcileRunManually below).
type ReconcileHandler struct {
	runs  domainsearch.RunStore
	jobs  domainsearch.ReindexJobLookup
	log   Logger
	clock func() time.Time
}

// NewReconcileHandler creates a ReconcileHandler. Panics if any dependency
// is nil, matching inventory.NewReservationExpiryHandler's convention for
// a job handler with no meaningful degraded mode.
func NewReconcileHandler(runs domainsearch.RunStore, jobLookup domainsearch.ReindexJobLookup, log Logger) *ReconcileHandler {
	if runs == nil {
		panic("search.NewReconcileHandler: nil runs")
	}
	if jobLookup == nil {
		panic("search.NewReconcileHandler: nil jobLookup")
	}
	if log == nil {
		panic("search.NewReconcileHandler: nil log")
	}
	return &ReconcileHandler{runs: runs, jobs: jobLookup, log: log, clock: time.Now}
}

// Type implements jobs.Handler.
func (h *ReconcileHandler) Type() string { return ReconcileJobType }

// Handle implements jobs.Handler. It returns an error when one or more
// stale runs could not be checked or corrected (a job-lookup or Finish
// failure that isn't the benign "already terminal" outcome) — a
// persistent failure here (e.g. a permissions or connectivity issue
// scoped to these tables/queries) must surface as a retried/failed job,
// not silently report "done" while doing nothing, the same reasoning
// inventory.ReservationExpiryHandler applies to its own genuine failures.
func (h *ReconcileHandler) Handle(ctx context.Context, _ domainjobs.Job) error {
	sweepCtx, cancel := context.WithTimeout(ctx, reconcileSweepTimeout)
	defer cancel()

	cutoff := h.clock().UTC().Add(-staleAfter)
	stale, err := h.runs.FindStaleProcessing(sweepCtx, cutoff, reconcileBatchLimit)
	if err != nil {
		return fmt.Errorf("search.reindex.reconcile: find stale runs: %w", err)
	}
	if len(stale) == 0 {
		return nil
	}

	var reconciled, stillWaiting, alreadyTerminal, errored int
	for _, run := range stale {
		if sweepCtx.Err() != nil {
			// Ran out of time partway through this batch — stop cleanly
			// and let the next tick pick up the remainder (each run left
			// untouched is still "processing" and so still matches
			// FindStaleProcessing next time), the same early-stop pattern
			// ReservationExpiryHandler uses for its own sweepTimeout.
			break
		}

		jobStatus, err := h.jobs.FindReindexJobByRunID(sweepCtx, run.ID)
		if err != nil {
			errored++
			h.log.Error("search.reindex.reconcile.job_lookup_failed", err, map[string]interface{}{"run_id": run.ID})
			continue
		}

		var lastErr string
		switch {
		case !jobStatus.Found:
			lastErr = "reindex run reconciled: no search.reindex job found for this run_id — it was likely never enqueued"
		case !jobStatus.Terminal:
			stillWaiting++
			h.log.Info("search.reindex.reconcile.job_not_terminal", map[string]interface{}{
				"run_id":     run.ID,
				"job_id":     jobStatus.JobID,
				"job_status": jobStatus.Status,
			})
			continue
		default:
			lastErr = fmt.Sprintf("reindex run reconciled: underlying job %s reached terminal status %q but never updated this run (worker crash or repeated status-write failure)", jobStatus.JobID, jobStatus.Status)
		}

		if err := h.runs.Finish(sweepCtx, run.ID, domainsearch.RunStatusFailed, lastErr); err != nil {
			if errors.Is(err, domainsearch.ErrRunNotProcessing) {
				// Someone else (a retried job completing, an overlapping
				// sweep, a manual reconcile) already finished this run
				// between FindStaleProcessing's read and this write — not
				// a failure, just a decision that's now moot.
				alreadyTerminal++
				h.log.Info("search.reindex.reconcile.already_terminal", map[string]interface{}{"run_id": run.ID})
				continue
			}
			errored++
			h.log.Error("search.reindex.reconcile.finish_failed", err, map[string]interface{}{"run_id": run.ID})
			continue
		}
		reconciled++
		h.log.Info("search.reindex.reconcile.corrected", map[string]interface{}{
			"run_id": run.ID,
			"reason": lastErr,
		})
	}

	h.log.Info("search.reindex.reconcile.complete", map[string]interface{}{
		"stale_found":      len(stale),
		"reconciled":       reconciled,
		"still_waiting":    stillWaiting,
		"already_terminal": alreadyTerminal,
		"errors":           errored,
		"more_remaining":   sweepCtx.Err() != nil || len(stale) == reconcileBatchLimit,
	})
	if errored > 0 {
		return fmt.Errorf("search.reindex.reconcile: %d of %d stale run(s) could not be checked or corrected", errored, len(stale))
	}
	return nil
}

// ReconcileRunManually flips a run stuck "processing" to "failed" with an
// operator-supplied reason — the CLI-triggered counterpart to
// ReconcileHandler's automatic sweep, for the ambiguous case that sweep
// deliberately leaves alone: a run whose underlying job is *also* still
// "processing" (or missing) can't be told apart, from status alone, from
// one still legitimately running. An operator who has independently
// confirmed the job is never coming back (e.g. the worker process that
// held it is confirmed dead, per RUNBOOK.md's "Search reindex" section)
// uses this to correct the run explicitly instead of editing the database
// by hand.
func ReconcileRunManually(ctx context.Context, runs domainsearch.RunStore, runID, reason string) error {
	if runID == "" {
		return fmt.Errorf("search: reconcile run: run id is required")
	}
	if reason == "" {
		return fmt.Errorf("search: reconcile run: reason is required")
	}

	run, err := runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("search: reconcile run: %w", err)
	}
	if run == nil {
		return apperror.NotFound(fmt.Sprintf("reindex run %q not found", runID))
	}
	if run.Status != domainsearch.RunStatusProcessing {
		return apperror.Conflict(fmt.Sprintf("reindex run %q is %s, not processing — only a processing run can be manually reconciled", runID, run.Status))
	}

	// The Get above is only a friendly up-front check (a clear message
	// naming the run's actual status) — Finish's own conditional update
	// is what actually guards against the run finishing concurrently
	// between that read and this write.
	if err := runs.Finish(ctx, runID, domainsearch.RunStatusFailed, reason); err != nil {
		if errors.Is(err, domainsearch.ErrRunNotProcessing) {
			return apperror.Conflict(fmt.Sprintf("reindex run %q finished concurrently before this could apply — check its current status", runID))
		}
		return fmt.Errorf("search: reconcile run: %w", err)
	}
	return nil
}
