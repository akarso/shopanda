package search_test

import (
	"context"
	"errors"
	"testing"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type fakeRunStore struct {
	created   domainsearch.Run
	createErr error

	runs map[string]*domainsearch.Run

	// finishErr, when set, is returned by Finish. finishFailTimes, when >
	// 0, limits that to only the first N calls (after which Finish
	// succeeds) — models a transient persistence blip that a bounded
	// in-process retry (ReindexHandler.finishWithRetry) recovers from.
	// Zero (the default) means finishErr, if set, fails every call.
	finishErr       error
	finishFailTimes int
	finishCalls     int

	// staleRuns/staleErr back FindStaleProcessing directly — the fake
	// doesn't derive staleness from runs/started_at itself, tests set up
	// exactly what a "stale" query should return.
	staleRuns      []domainsearch.Run
	staleErr       error
	findStaleLimit int
}

// findStaleLimit records the limit passed to the most recent
// FindStaleProcessing call, so tests can assert the sweep bounds its own
// query rather than reading unboundedly.
func (f *fakeRunStore) FindStaleProcessing(_ context.Context, _ time.Time, limit int) ([]domainsearch.Run, error) {
	f.findStaleLimit = limit
	if f.staleErr != nil {
		return nil, f.staleErr
	}
	return f.staleRuns, nil
}

func (f *fakeRunStore) Create(_ context.Context, run domainsearch.Run) error {
	f.created = run
	if f.createErr != nil {
		return f.createErr
	}
	if f.runs == nil {
		f.runs = map[string]*domainsearch.Run{}
	}
	r := run
	f.runs[run.ID] = &r
	return nil
}

func (f *fakeRunStore) Get(_ context.Context, id string) (*domainsearch.Run, error) {
	return f.runs[id], nil
}

func (f *fakeRunStore) UpdateProgress(_ context.Context, id string, total, processed, errCount int) error {
	r := f.runs[id]
	if r == nil {
		return errors.New("no such run")
	}
	r.TotalCount, r.ProcessedCount, r.ErrorCount = total, processed, errCount
	return nil
}

// Finish mirrors SearchIndexRunRepo.Finish's conditional-update contract:
// it only actually transitions a run currently "processing", returning
// domainsearch.ErrRunNotProcessing otherwise (found but not processing) so
// tests can exercise the same race-guard behavior callers depend on.
func (f *fakeRunStore) Finish(_ context.Context, id string, status domainsearch.RunStatus, lastErr string) error {
	f.finishCalls++
	if f.finishErr != nil && (f.finishFailTimes <= 0 || f.finishCalls <= f.finishFailTimes) {
		return f.finishErr
	}
	r := f.runs[id]
	if r == nil {
		return errors.New("no such run")
	}
	if r.Status != domainsearch.RunStatusProcessing {
		return domainsearch.ErrRunNotProcessing
	}
	r.Status = status
	r.LastError = lastErr
	return nil
}

type fakeQueue struct {
	enqueued   []domainjobs.Job
	enqueueErr error
}

func (f *fakeQueue) Enqueue(_ context.Context, job domainjobs.Job) error {
	f.enqueued = append(f.enqueued, job)
	return f.enqueueErr
}
func (f *fakeQueue) Dequeue(context.Context) (*domainjobs.Job, error) { return nil, nil }
func (f *fakeQueue) Complete(context.Context, string) error           { return nil }
func (f *fakeQueue) Fail(context.Context, string, error) error        { return nil }

func TestNewReindexService_NilDeps(t *testing.T) {
	if _, err := searchApp.NewReindexService(nil, &fakeQueue{}, logger.New("error")); err == nil {
		t.Fatal("expected error for nil store")
	}
	if _, err := searchApp.NewReindexService(&fakeRunStore{}, nil, logger.New("error")); err == nil {
		t.Fatal("expected error for nil queue")
	}
	if _, err := searchApp.NewReindexService(&fakeRunStore{}, &fakeQueue{}, nil); err == nil {
		t.Fatal("expected error for nil log")
	}
}

func TestReindexService_Trigger_CreatesRunAndEnqueuesJob(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, queue, logger.New("error"))
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}

	runID, err := svc.Trigger(context.Background(), domainsearch.ReindexScope{Name: "all"})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if runID == "" {
		t.Fatal("expected a non-empty run ID")
	}
	if store.created.ID != runID || store.created.Scope != "all" || store.created.Status != domainsearch.RunStatusProcessing {
		t.Errorf("created run = %+v, want id=%s scope=all status=processing", store.created, runID)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
	job := queue.enqueued[0]
	if job.Type != searchApp.JobType {
		t.Errorf("job.Type = %q, want %q", job.Type, searchApp.JobType)
	}
	if job.Payload["run_id"] != runID {
		t.Errorf("job payload run_id = %v, want %q", job.Payload["run_id"], runID)
	}
}

func TestReindexService_Trigger_EmptyScopeErrors(t *testing.T) {
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, &fakeQueue{}, logger.New("error"))
	if _, err := svc.Trigger(context.Background(), domainsearch.ReindexScope{}); err == nil {
		t.Fatal("expected error for empty scope name")
	}
}

func TestReindexService_Trigger_StoreCreateErrorPropagates(t *testing.T) {
	store := &fakeRunStore{createErr: errors.New("db down")}
	svc, _ := searchApp.NewReindexService(store, &fakeQueue{}, logger.New("error"))
	if _, err := svc.Trigger(context.Background(), domainsearch.ReindexScope{Name: "all"}); err == nil {
		t.Fatal("expected the store's error to propagate")
	}
}

// TestReindexService_Trigger_QueueEnqueueErrorPropagates_MarksRunFailed pins
// the fix for a real orphaning bug: Create and Enqueue are two independent,
// non-transactional steps, so an Enqueue failure after Create succeeded
// used to leave the run row stuck at "processing" forever, with no job
// ever created to finish it — indistinguishable from a crashed worker, but
// with nothing to reconcile against. Trigger must compensate by marking
// the run failed itself when this happens.
func TestReindexService_Trigger_QueueEnqueueErrorPropagates_MarksRunFailed(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{enqueueErr: errors.New("queue unavailable")}
	svc, _ := searchApp.NewReindexService(store, queue, logger.New("error"))

	runID, err := svc.Trigger(context.Background(), domainsearch.ReindexScope{Name: "all"})
	if err == nil {
		t.Fatal("expected the queue's error to propagate")
	}
	if runID != "" {
		t.Errorf("Trigger returned a non-empty run ID (%q) alongside an error", runID)
	}
	run := store.created
	got := store.runs[run.ID]
	if got == nil {
		t.Fatalf("no run row found for id %q", run.ID)
	}
	if got.Status != domainsearch.RunStatusFailed {
		t.Errorf("run status after a failed enqueue = %q, want failed (not left orphaned processing)", got.Status)
	}
	if got.LastError == "" {
		t.Error("expected LastError to be set on the compensated run")
	}
}
