package search_test

import (
	"context"
	"errors"
	"testing"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type fakeJobLookup struct {
	// byRunID maps run_id -> status to return. A run_id absent from the
	// map returns Found: false.
	byRunID map[string]domainsearch.ReindexJobStatus
	err     error
}

func (f *fakeJobLookup) FindReindexJobByRunID(_ context.Context, runID string) (domainsearch.ReindexJobStatus, error) {
	if f.err != nil {
		return domainsearch.ReindexJobStatus{}, f.err
	}
	return f.byRunID[runID], nil
}

func TestReconcileHandler_Type(t *testing.T) {
	h := searchApp.NewReconcileHandler(&fakeRunStore{}, &fakeJobLookup{}, logger.New("error"))
	if h.Type() != searchApp.ReconcileJobType {
		t.Errorf("Type() = %q, want %q", h.Type(), searchApp.ReconcileJobType)
	}
}

func TestReconcileHandler_Handle_NoStaleRuns(t *testing.T) {
	store := &fakeRunStore{}
	h := searchApp.NewReconcileHandler(store, &fakeJobLookup{}, logger.New("error"))
	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func TestReconcileHandler_Handle_TerminalJobCorrectsStuckRun(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
		staleRuns: []domainsearch.Run{{ID: "run-1", Status: domainsearch.RunStatusProcessing}},
	}
	jobs := &fakeJobLookup{byRunID: map[string]domainsearch.ReindexJobStatus{
		"run-1": {Found: true, JobID: "job-1", Status: "failed", Terminal: true},
	}}
	h := searchApp.NewReconcileHandler(store, jobs, logger.New("error"))

	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := store.runs["run-1"]
	if got.Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed", got.Status)
	}
	if got.LastError == "" {
		t.Error("expected LastError to be set explaining the reconciliation")
	}
}

func TestReconcileHandler_Handle_MissingJobCorrectsStuckRun(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
		staleRuns: []domainsearch.Run{{ID: "run-1", Status: domainsearch.RunStatusProcessing}},
	}
	h := searchApp.NewReconcileHandler(store, &fakeJobLookup{}, logger.New("error"))

	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed", store.runs["run-1"].Status)
	}
}

func TestReconcileHandler_Handle_NonTerminalJobLeavesRunAlone(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
		staleRuns: []domainsearch.Run{{ID: "run-1", Status: domainsearch.RunStatusProcessing}},
	}
	jobs := &fakeJobLookup{byRunID: map[string]domainsearch.ReindexJobStatus{
		"run-1": {Found: true, JobID: "job-1", Status: "processing", Terminal: false},
	}}
	h := searchApp.NewReconcileHandler(store, jobs, logger.New("error"))

	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Errorf("run status = %q, want still processing (job hasn't reached a terminal state)", store.runs["run-1"].Status)
	}
}

func TestReconcileHandler_Handle_FindStaleErrorPropagates(t *testing.T) {
	store := &fakeRunStore{staleErr: errors.New("db down")}
	h := searchApp.NewReconcileHandler(store, &fakeJobLookup{}, logger.New("error"))
	if err := h.Handle(context.Background(), domainjobs.Job{}); err == nil {
		t.Fatal("expected an error when FindStaleProcessing fails")
	}
}

func TestReconcileHandler_Handle_JobLookupErrorLeavesRunUntouchedButFailsJob(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
		staleRuns: []domainsearch.Run{{ID: "run-1", Status: domainsearch.RunStatusProcessing}},
	}
	jobs := &fakeJobLookup{err: errors.New("db down")}
	h := searchApp.NewReconcileHandler(store, jobs, logger.New("error"))

	// A persistent lookup failure must not guess at the run's outcome —
	// it's left untouched — but it also must not read as a silent
	// success: Handle propagates the failure so the job is retried/failed
	// like any other genuine error, instead of the sweep reporting "done"
	// while a connectivity/permissions issue quietly does nothing.
	err := h.Handle(context.Background(), domainjobs.Job{})
	if err == nil {
		t.Fatal("expected Handle to return an error when a stale run's job lookup fails")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Errorf("run status = %q, want unchanged (job lookup failed, must not guess)", store.runs["run-1"].Status)
	}
}

func TestReconcileHandler_Handle_ConcurrentlyFinishedRunIsSkippedNotErrored(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			// Simulates the exact race the conditional-Finish guard
			// exists for: the stale read below still shows "processing",
			// but the run has since finished concurrently (e.g. a
			// retried job completing) by the time Finish actually runs.
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusCompleted},
		},
		staleRuns: []domainsearch.Run{{ID: "run-1", Status: domainsearch.RunStatusProcessing}},
	}
	jobs := &fakeJobLookup{byRunID: map[string]domainsearch.ReindexJobStatus{
		"run-1": {Found: true, JobID: "job-1", Status: "failed", Terminal: true},
	}}
	h := searchApp.NewReconcileHandler(store, jobs, logger.New("error"))

	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := store.runs["run-1"]
	if got.Status != domainsearch.RunStatusCompleted {
		t.Errorf("status = %q, want still completed (must not be clobbered by a stale decision)", got.Status)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want still empty (must not be clobbered)", got.LastError)
	}
}

func TestReconcileHandler_Handle_BoundsFindStaleProcessingQuery(t *testing.T) {
	store := &fakeRunStore{}
	h := searchApp.NewReconcileHandler(store, &fakeJobLookup{}, logger.New("error"))

	if err := h.Handle(context.Background(), domainjobs.Job{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.findStaleLimit != 500 {
		t.Errorf("FindStaleProcessing limit = %d, want 500 (reconcileBatchLimit) — a mass-orphan event must not read unbounded", store.findStaleLimit)
	}
}

func TestNewReconcileHandler_PanicsOnNilDeps(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: expected a panic for a nil dependency", name)
				}
			}()
			fn()
		})
	}
	mustPanic("nil runs", func() { searchApp.NewReconcileHandler(nil, &fakeJobLookup{}, logger.New("error")) })
	mustPanic("nil jobLookup", func() { searchApp.NewReconcileHandler(&fakeRunStore{}, nil, logger.New("error")) })
	mustPanic("nil log", func() { searchApp.NewReconcileHandler(&fakeRunStore{}, &fakeJobLookup{}, nil) })
}

func TestReconcileRunManually_FailsProcessingRun(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
	}
	if err := searchApp.ReconcileRunManually(context.Background(), store, "run-1", "worker confirmed dead"); err != nil {
		t.Fatalf("ReconcileRunManually: %v", err)
	}
	got := store.runs["run-1"]
	if got.Status != domainsearch.RunStatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.LastError != "worker confirmed dead" {
		t.Errorf("LastError = %q, want the operator-supplied reason", got.LastError)
	}
}

func TestReconcileRunManually_NotFound(t *testing.T) {
	store := &fakeRunStore{runs: map[string]*domainsearch.Run{}}
	err := searchApp.ReconcileRunManually(context.Background(), store, "missing", "reason")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("expected a not_found error, got %v", err)
	}
}

func TestReconcileRunManually_RejectsNonProcessingRun(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusCompleted},
		},
	}
	err := searchApp.ReconcileRunManually(context.Background(), store, "run-1", "reason")
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected a conflict error, got %v", err)
	}
}

func TestReconcileRunManually_ConcurrentlyFinishedRunReturnsConflict(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			// Get() below still observes "processing" (passes the
			// friendly pre-check), but Finish's own conditional update
			// must catch a status change that happens between that read
			// and the write — simulated here by finishErr forcing
			// ErrRunNotProcessing regardless of the map's current value.
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
		finishErr: domainsearch.ErrRunNotProcessing,
	}
	err := searchApp.ReconcileRunManually(context.Background(), store, "run-1", "reason")
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("expected a conflict error for a concurrently-finished run, got %v", err)
	}
}

func TestReconcileRunManually_RequiresReason(t *testing.T) {
	store := &fakeRunStore{
		runs: map[string]*domainsearch.Run{
			"run-1": {ID: "run-1", Status: domainsearch.RunStatusProcessing},
		},
	}
	if err := searchApp.ReconcileRunManually(context.Background(), store, "run-1", ""); err == nil {
		t.Fatal("expected an error for an empty reason")
	}
}
