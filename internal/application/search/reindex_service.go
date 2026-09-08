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

// ReindexService creates a search_index_runs row and enqueues the job that
// will actually perform it, returning the run ID immediately — the caller
// does not wait for the run to finish (that's runSearchReindex's --wait,
// which polls RunStore.Get separately).
type ReindexService struct {
	store domainsearch.RunStore
	queue domainjobs.Queue
	log   Logger
}

// NewReindexService creates a ReindexService backed by store and queue.
func NewReindexService(store domainsearch.RunStore, queue domainjobs.Queue, log Logger) (*ReindexService, error) {
	if store == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil store")
	}
	if queue == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil queue")
	}
	if log == nil {
		return nil, fmt.Errorf("search.NewReindexService: nil log")
	}
	return &ReindexService{store: store, queue: queue, log: log}, nil
}

// Trigger creates a run row and enqueues the search.reindex job for it,
// returning the new run's ID.
func (s *ReindexService) Trigger(ctx context.Context, scope domainsearch.ReindexScope) (string, error) {
	if scope.Name == "" {
		return "", fmt.Errorf("search: trigger: empty scope")
	}
	params := scope.Params
	if params == nil {
		params = map[string]interface{}{}
	}

	runID := id.New()
	run := domainsearch.Run{
		ID:          runID,
		Scope:       scope.Name,
		ScopeParams: params,
		Status:      domainsearch.RunStatusProcessing,
		StartedAt:   time.Now().UTC(),
	}
	if err := s.store.Create(ctx, run); err != nil {
		return "", fmt.Errorf("search: trigger: create run: %w", err)
	}

	job, err := domainjobs.NewJob(id.New(), JobType, map[string]interface{}{
		"run_id": runID,
		"scope":  scope.Name,
	})
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
