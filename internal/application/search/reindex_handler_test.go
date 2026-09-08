package search_test

import (
	"context"
	"errors"
	"testing"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type fakeProductSource struct {
	products        []domainsearch.Product
	countErr        error
	listErr         error
	listErrAtOffset int // -1 (default) means never
}

func (f *fakeProductSource) CountAll(context.Context) (int, error) {
	return len(f.products), f.countErr
}

func (f *fakeProductSource) ListAll(_ context.Context, offset, limit int) ([]domainsearch.Product, error) {
	if f.listErr != nil && offset == f.listErrAtOffset {
		return nil, f.listErr
	}
	if offset >= len(f.products) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.products) {
		end = len(f.products)
	}
	return f.products[offset:end], nil
}

type fakeSearchEngine struct {
	indexed []string
	failID  string
	failErr error
	// failFirstNCalls, when > 0, fails the first N calls to IndexProduct
	// (regardless of product ID) before succeeding — models a transient
	// error a retried attempt goes on to recover from.
	failFirstNCalls int
	calls           int
}

func (f *fakeSearchEngine) Name() string { return "fake" }
func (f *fakeSearchEngine) IndexProduct(_ context.Context, p domainsearch.Product) error {
	f.calls++
	if f.failFirstNCalls > 0 && f.calls <= f.failFirstNCalls {
		return f.failErr
	}
	if p.ID == f.failID {
		return f.failErr
	}
	f.indexed = append(f.indexed, p.ID)
	return nil
}
func (f *fakeSearchEngine) RemoveProduct(context.Context, string) error { return nil }
func (f *fakeSearchEngine) Search(context.Context, domainsearch.SearchQuery) (domainsearch.SearchResult, error) {
	return domainsearch.SearchResult{}, nil
}
func (f *fakeSearchEngine) Suggest(context.Context, string, int) ([]domainsearch.Suggestion, error) {
	return nil, nil
}

func newTestRun(store *fakeRunStore, id string) {
	if store.runs == nil {
		store.runs = map[string]*domainsearch.Run{}
	}
	run := domainsearch.Run{ID: id, Scope: "all", Status: domainsearch.RunStatusProcessing}
	store.runs[id] = &run
}

func TestReindexHandler_Type(t *testing.T) {
	h := searchApp.NewReindexHandler(&fakeRunStore{}, &fakeProductSource{}, &fakeSearchEngine{}, logger.New("error"))
	if h.Type() != searchApp.JobType {
		t.Errorf("Type() = %q, want %q", h.Type(), searchApp.JobType)
	}
}

func TestReindexHandler_Handle_MissingRunID(t *testing.T) {
	h := searchApp.NewReindexHandler(&fakeRunStore{}, &fakeProductSource{}, &fakeSearchEngine{}, logger.New("error"))
	job := domainjobs.Job{ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{}}
	if err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("expected an error for a job with no run_id in its payload")
	}
}

func TestReindexHandler_Handle_IndexesEveryProductAndCompletes(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{
		{ID: "p1"}, {ID: "p2"}, {ID: "p3"},
	}}
	engine := &fakeSearchEngine{}
	h := searchApp.NewReindexHandler(store, source, engine, logger.New("error"))

	job := domainjobs.Job{ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"}}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(engine.indexed) != 3 {
		t.Errorf("indexed %d products, want 3", len(engine.indexed))
	}
	run := store.runs["run-1"]
	if run.Status != domainsearch.RunStatusCompleted {
		t.Errorf("run status = %q, want completed", run.Status)
	}
	if run.ProcessedCount != 3 || run.TotalCount != 3 {
		t.Errorf("run counts = processed=%d total=%d, want 3/3", run.ProcessedCount, run.TotalCount)
	}
}

// finalAttemptJob returns a job payload for a run's last allowed attempt
// (Attempts == MaxRetries) — the same condition jobs.Queue.Fail uses to
// decide a permanent failure, and the condition failIfTerminal checks
// before marking a run row failed. Every "...FailsRun" test below uses
// this so the run is only expected to be marked failed on a terminal
// attempt, not any attempt — see TestReindexHandler_Handle_TransientFailureLeavesRunProcessing
// for the non-terminal case.
func finalAttemptJob(runID string) domainjobs.Job {
	return domainjobs.Job{
		ID:         "job-1",
		Type:       searchApp.JobType,
		Payload:    map[string]interface{}{"run_id": runID},
		Attempts:   3,
		MaxRetries: 3,
	}
}

func TestReindexHandler_Handle_IndexErrorFailsRun(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}, {ID: "p2"}}}
	engine := &fakeSearchEngine{failID: "p2", failErr: errors.New("index down")}
	h := searchApp.NewReindexHandler(store, source, engine, logger.New("error"))

	err := h.Handle(context.Background(), finalAttemptJob("run-1"))
	if err == nil {
		t.Fatal("expected the indexing error to propagate (so the job itself is retried)")
	}

	run := store.runs["run-1"]
	if run.Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed", run.Status)
	}
	if run.LastError == "" {
		t.Error("expected LastError to be set on a failed run")
	}
}

func TestReindexHandler_Handle_ListErrorFailsRun(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{
		products: []domainsearch.Product{{ID: "p1"}},
		listErr:  errors.New("connection reset"),
	}
	h := searchApp.NewReindexHandler(store, source, &fakeSearchEngine{}, logger.New("error"))

	if err := h.Handle(context.Background(), finalAttemptJob("run-1")); err == nil {
		t.Fatal("expected the list error to propagate")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed", store.runs["run-1"].Status)
	}
}

func TestReindexHandler_Handle_CountAllErrorFailsRun(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{countErr: errors.New("db down")}
	h := searchApp.NewReindexHandler(store, source, &fakeSearchEngine{}, logger.New("error"))

	if err := h.Handle(context.Background(), finalAttemptJob("run-1")); err == nil {
		t.Fatal("expected the count error to propagate")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed", store.runs["run-1"].Status)
	}
}

// TestReindexHandler_Handle_TransientFailureLeavesRunProcessing pins the
// Blocker fix: an error on a non-terminal attempt (the job still has
// retries left) must not mark the run failed — the worker's own retry
// may still succeed, and a run wrongly marked failed while a retry is in
// flight is a false signal to --wait/any progress view.
func TestReindexHandler_Handle_TransientFailureLeavesRunProcessing(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}}}
	engine := &fakeSearchEngine{failID: "p1", failErr: errors.New("transient index error")}
	h := searchApp.NewReindexHandler(store, source, engine, logger.New("error"))

	job := domainjobs.Job{
		ID:         "job-1",
		Type:       searchApp.JobType,
		Payload:    map[string]interface{}{"run_id": "run-1"},
		Attempts:   1,
		MaxRetries: 3,
	}
	if err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("expected the indexing error to propagate (so the worker retries)")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Errorf("run status after a non-terminal attempt = %q, want still processing", store.runs["run-1"].Status)
	}
}

// TestReindexHandler_Handle_TransientFailureThenRetrySucceeds simulates the
// full scenario behind the Blocker: attempt 1 hits a transient error (run
// stays processing), the worker retries the same job, attempt 2 succeeds
// and completes the run — exactly the case an eager Finish(failed) on
// every attempt would have permanently misreported as failed.
func TestReindexHandler_Handle_TransientFailureThenRetrySucceeds(t *testing.T) {
	store := &fakeRunStore{}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}, {ID: "p2"}}}
	engine := &fakeSearchEngine{failErr: errors.New("transient index error"), failFirstNCalls: 1}
	h := searchApp.NewReindexHandler(store, source, engine, logger.New("error"))

	attempt1 := domainjobs.Job{
		ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"},
		Attempts: 1, MaxRetries: 3,
	}
	if err := h.Handle(context.Background(), attempt1); err == nil {
		t.Fatal("expected attempt 1 to return an error (its first IndexProduct call fails)")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Fatalf("run status after attempt 1 = %q, want still processing", store.runs["run-1"].Status)
	}

	attempt2 := domainjobs.Job{
		ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"},
		Attempts: 2, MaxRetries: 3,
	}
	if err := h.Handle(context.Background(), attempt2); err != nil {
		t.Fatalf("Handle (attempt 2): %v", err)
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusCompleted {
		t.Errorf("run status after a successful retry = %q, want completed", store.runs["run-1"].Status)
	}
}

// TestReindexHandler_Handle_FinishCompletedRetriesThenSucceeds pins
// finishWithRetry's actual retry behavior: a Finish(completed) call that
// fails transiently (fewer times than the retry budget) must still
// persist "completed" within the same Handle call, rather than treating
// the first failure as final.
func TestReindexHandler_Handle_FinishCompletedRetriesThenSucceeds(t *testing.T) {
	store := &fakeRunStore{finishErr: errors.New("deadlock detected"), finishFailTimes: 2}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}}}
	h := searchApp.NewReindexHandler(store, source, &fakeSearchEngine{}, logger.New("error"))

	job := domainjobs.Job{ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"}, Attempts: 1, MaxRetries: 3}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v (want the retry to recover before the budget runs out)", err)
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusCompleted {
		t.Errorf("run status = %q, want completed", store.runs["run-1"].Status)
	}
	if store.finishCalls < 3 {
		t.Errorf("Finish called %d times, want at least 3 (2 failures + 1 success)", store.finishCalls)
	}
}

// TestReindexHandler_Handle_FinishCompletedFailsPermanently_NonTerminalAttempt
// pins the Blocker fix: if Finish(completed) can't be persisted at all
// (every retry fails) on a non-final attempt, Handle must return an error
// — completing the job here regardless would leave the run stuck
// "processing" forever with nothing left to retry it.
func TestReindexHandler_Handle_FinishCompletedFailsPermanently_NonTerminalAttempt(t *testing.T) {
	store := &fakeRunStore{finishErr: errors.New("db down")}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}}}
	h := searchApp.NewReindexHandler(store, source, &fakeSearchEngine{}, logger.New("error"))

	job := domainjobs.Job{ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"}, Attempts: 1, MaxRetries: 3}
	if err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("expected Handle to return an error when Finish(completed) can't be persisted, so the job is retried instead of completed")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Errorf("run status = %q, want still processing (a retry is still available)", store.runs["run-1"].Status)
	}
}

// TestReindexHandler_Handle_FinishCompletedFailsPermanently_TerminalAttempt
// covers the worst case both terminal Finish call sites share: the store
// is down entirely, so even failIfTerminal's own compensating
// Finish(failed) call fails. Handle must still return an error (so the
// job itself is correctly marked failed by the queue) rather than panic
// or silently report success — the run row is left processing, which is
// an honest reflection of "we could not persist anything," not a crash.
func TestReindexHandler_Handle_FinishCompletedFailsPermanently_TerminalAttempt(t *testing.T) {
	store := &fakeRunStore{finishErr: errors.New("db down")}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}}}
	h := searchApp.NewReindexHandler(store, source, &fakeSearchEngine{}, logger.New("error"))

	job := domainjobs.Job{ID: "job-1", Type: searchApp.JobType, Payload: map[string]interface{}{"run_id": "run-1"}, Attempts: 3, MaxRetries: 3}
	if err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("expected Handle to return an error")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusProcessing {
		t.Errorf("run status = %q, want still processing (every Finish attempt failed — nothing could be persisted)", store.runs["run-1"].Status)
	}
}

// TestReindexHandler_Handle_FailIfTerminalFinishRetriesThenSucceeds pins
// finishWithRetry's retry behavior on the failIfTerminal path too (not
// just the completed path): a terminal-attempt Finish(failed) call that
// fails transiently must still end up persisted as failed.
func TestReindexHandler_Handle_FailIfTerminalFinishRetriesThenSucceeds(t *testing.T) {
	store := &fakeRunStore{finishErr: errors.New("deadlock detected"), finishFailTimes: 2}
	newTestRun(store, "run-1")
	source := &fakeProductSource{products: []domainsearch.Product{{ID: "p1"}, {ID: "p2"}}}
	engine := &fakeSearchEngine{failID: "p2", failErr: errors.New("index down")}
	h := searchApp.NewReindexHandler(store, source, engine, logger.New("error"))

	err := h.Handle(context.Background(), finalAttemptJob("run-1"))
	if err == nil {
		t.Fatal("expected the indexing error to propagate")
	}
	if store.runs["run-1"].Status != domainsearch.RunStatusFailed {
		t.Errorf("run status = %q, want failed (Finish should have recovered within its retry budget)", store.runs["run-1"].Status)
	}
}
