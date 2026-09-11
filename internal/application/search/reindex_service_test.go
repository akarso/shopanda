package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// defaultThreshold mirrors config.DefaultSearchReindexFullScanThreshold —
// duplicated here (not imported: platform/config would be an odd
// dependency for this package's tests) so threshold-boundary tests read
// against the same documented default production actually runs with.
const defaultThreshold = 0.2

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

func (f *fakeRunStore) List(context.Context, int, int) ([]domainsearch.Run, error) {
	return nil, nil
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
	if _, err := searchApp.NewReindexService(nil, &fakeProductSource{}, &fakeQueue{}, logger.New("error"), defaultThreshold); err == nil {
		t.Fatal("expected error for nil store")
	}
	if _, err := searchApp.NewReindexService(&fakeRunStore{}, nil, &fakeQueue{}, logger.New("error"), defaultThreshold); err == nil {
		t.Fatal("expected error for nil products")
	}
	if _, err := searchApp.NewReindexService(&fakeRunStore{}, &fakeProductSource{}, nil, logger.New("error"), defaultThreshold); err == nil {
		t.Fatal("expected error for nil queue")
	}
	if _, err := searchApp.NewReindexService(&fakeRunStore{}, &fakeProductSource{}, &fakeQueue{}, nil, defaultThreshold); err == nil {
		t.Fatal("expected error for nil log")
	}
}

func TestReindexService_Trigger_CreatesRunAndEnqueuesJob(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, &fakeProductSource{}, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeAll{})
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
	if job.Payload["scope"] != "all" {
		t.Errorf("job payload scope = %v, want \"all\"", job.Payload["scope"])
	}
	if _, ok := job.Payload["product_ids"]; ok {
		t.Error("job payload should not carry product_ids for a full-scan run")
	}
}

func TestReindexService_Trigger_StoreCreateErrorPropagates(t *testing.T) {
	store := &fakeRunStore{createErr: errors.New("db down")}
	svc, _ := searchApp.NewReindexService(store, &fakeProductSource{}, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeAll{}); err == nil {
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
	svc, _ := searchApp.NewReindexService(store, &fakeProductSource{}, queue, logger.New("error"), defaultThreshold)

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeAll{})
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

// --- Scope resolution (PR-1034) ---

func TestReindexService_Trigger_ScopeProducts_RunsScoped(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	products := &fakeProductSource{products: make([]domainsearch.Product, 100)} // total catalog = 100
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	ids := []string{id.New(), id.New()} // 2/100 = 2%, well under the 20% threshold
	runID, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: ids})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	run := store.runs[runID]
	if run.Scope != "products" {
		t.Errorf("run.Scope = %q, want products", run.Scope)
	}
	if run.ScopeParams["requested_scope"] != "products" {
		t.Errorf("run.ScopeParams[requested_scope] = %v, want products", run.ScopeParams["requested_scope"])
	}

	job := queue.enqueued[0]
	if job.Payload["scope"] != "products" {
		t.Errorf("job payload scope = %v, want products", job.Payload["scope"])
	}
	got, ok := job.Payload["product_ids"].([]string)
	if !ok || len(got) != 2 {
		t.Errorf("job payload product_ids = %v, want %v", job.Payload["product_ids"], ids)
	}
}

// TestReindexService_Trigger_ScopeProducts_DeduplicatesAndValidatesIDs pins
// the fix for a duplicate-ID bug: without deduplication, a repeated ID
// would inflate the ratio fed to applyThreshold and the run's total_count
// beyond what ListByIDs' own `= ANY($1)` (which naturally deduplicates on
// the read side) could ever report back as processed_count — a
// "completed" run permanently showing e.g. 1/2 instead of 1/1. Also pins
// that a blank or non-UUID entry is rejected synchronously here, not
// surfaced later as a Postgres error after the job is already enqueued.
func TestReindexService_Trigger_ScopeProducts_DeduplicatesAndValidatesIDs(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	products := &fakeProductSource{products: make([]domainsearch.Product, 100)}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	dupID := id.New()
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: []string{dupID, dupID}}); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	job := queue.enqueued[0]
	got, ok := job.Payload["product_ids"].([]string)
	if !ok || len(got) != 1 {
		t.Fatalf("job payload product_ids = %v, want exactly 1 deduplicated id", job.Payload["product_ids"])
	}

	if _, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: []string{""}}); err == nil {
		t.Error("expected an error for a blank product id")
	}
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: []string{"not-a-uuid"}}); err == nil {
		t.Error("expected an error for a non-UUID product id")
	}
}

func TestReindexService_Trigger_ScopeProducts_EmptyIDsErrors(t *testing.T) {
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, &fakeProductSource{}, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{}); err == nil {
		t.Fatal("expected an error for an empty product ID list")
	}
}

func TestReindexService_Trigger_ScopeCategories_ResolvesToProductIDs(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	products := &fakeProductSource{
		products:           make([]domainsearch.Product, 100),
		categoryProductIDs: []string{"p1", "p2", "p3"},
	}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeCategories{IDs: []string{"cat-1"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	run := store.runs[runID]
	if run.Scope != "products" {
		t.Errorf("run.Scope = %q, want products", run.Scope)
	}
	if run.ScopeParams["requested_scope"] != "categories" {
		t.Errorf("run.ScopeParams[requested_scope] = %v, want categories", run.ScopeParams["requested_scope"])
	}
	job := queue.enqueued[0]
	got, ok := job.Payload["product_ids"].([]string)
	if !ok || len(got) != 3 {
		t.Errorf("job payload product_ids = %v, want the 3 resolved product ids", job.Payload["product_ids"])
	}
}

func TestReindexService_Trigger_ScopeCategories_EmptyIDsErrors(t *testing.T) {
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, &fakeProductSource{}, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeCategories{}); err == nil {
		t.Fatal("expected an error for an empty category ID list")
	}
}

func TestReindexService_Trigger_ScopeCategories_ResolveErrorPropagates(t *testing.T) {
	products := &fakeProductSource{categoryErr: errors.New("db down")}
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, products, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeCategories{IDs: []string{"cat-1"}}); err == nil {
		t.Fatal("expected the category resolution error to propagate")
	}
}

func TestReindexService_Trigger_ScopeSince_ResolvesToProductIDs(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	products := &fakeProductSource{
		products:        make([]domainsearch.Product, 100),
		updatedSinceIDs: []string{"p1"},
	}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runID, err := svc.Trigger(context.Background(), searchApp.ScopeSince{Since: since})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	run := store.runs[runID]
	if run.Scope != "products" {
		t.Errorf("run.Scope = %q, want products", run.Scope)
	}
	if run.ScopeParams["requested_scope"] != "since" {
		t.Errorf("run.ScopeParams[requested_scope] = %v, want since", run.ScopeParams["requested_scope"])
	}
	if run.ScopeParams["since"] != since.Format(time.RFC3339) {
		t.Errorf("run.ScopeParams[since] = %v, want %s", run.ScopeParams["since"], since.Format(time.RFC3339))
	}
}

func TestReindexService_Trigger_ScopeSince_ZeroTimeErrors(t *testing.T) {
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, &fakeProductSource{}, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeSince{}); err == nil {
		t.Fatal("expected an error for a zero-value Since timestamp")
	}
}

func TestReindexService_Trigger_ScopeSince_ResolveErrorPropagates(t *testing.T) {
	products := &fakeProductSource{updatedSinceErr: errors.New("db down")}
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, products, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeSince{Since: time.Now()}); err == nil {
		t.Fatal("expected the since resolution error to propagate")
	}
}

// TestReindexService_Trigger_EmptyResolutionSkipsCountAll pins that an
// empty resolved ID list (e.g. every product in the requested categories
// was deleted) doesn't need — and doesn't make — a CountAll call:
// comparing 0 resolved IDs against any catalog size always keeps the
// scope, so consulting the catalog size would be pure overhead. Proven by
// making CountAll itself fail: if Trigger still succeeds, it never called
// it.
func TestReindexService_Trigger_EmptyResolutionSkipsCountAll(t *testing.T) {
	products := &fakeProductSource{countErr: errors.New("CountAll must not be called for an empty resolved scope")}
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, products, &fakeQueue{}, logger.New("error"), defaultThreshold)

	if _, err := svc.Trigger(context.Background(), searchApp.ScopeCategories{IDs: []string{"cat-1"}}); err != nil {
		t.Fatalf("Trigger: %v (CountAll should not have been consulted for an empty resolved scope)", err)
	}
}

// TestReindexService_Trigger_EmptyResolvedScope_ProductIDsSurvivesJSONRoundTrip
// pins the fix for a bug fakeQueue's in-memory Enqueue can't otherwise
// catch: a nil []string (Go's zero value, easy to produce from an empty
// resolution) marshals to JSON `null`, and decoding `null` back into a
// job payload's generic map[string]interface{} gives an untyped nil, not
// an empty []interface{} — so scopedProductIDs' own
// `.([]interface{})` type assertion would fail and the job would error
// with "requires a product_ids array" instead of completing a
// legitimate, if trivial, 0/0 run. Proven here by actually round-tripping
// the enqueued payload through encoding/json, the same transformation a
// real Postgres JSONB column applies.
func TestReindexService_Trigger_EmptyResolvedScope_ProductIDsSurvivesJSONRoundTrip(t *testing.T) {
	queue := &fakeQueue{}
	products := &fakeProductSource{categoryProductIDs: nil} // resolves to zero matches
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, products, queue, logger.New("error"), defaultThreshold)

	if _, err := svc.Trigger(context.Background(), searchApp.ScopeCategories{IDs: []string{"cat-1"}}); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	raw, err := json.Marshal(queue.enqueued[0].Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	ids, ok := decoded["product_ids"].([]interface{})
	if !ok {
		t.Fatalf("product_ids decoded as %T (%v), want []interface{} (an empty JSON array, not null)", decoded["product_ids"], decoded["product_ids"])
	}
	if len(ids) != 0 {
		t.Errorf("product_ids = %v, want empty", ids)
	}
}

func TestReindexService_Trigger_CountAllErrorPropagates(t *testing.T) {
	products := &fakeProductSource{countErr: errors.New("db down")}
	svc, _ := searchApp.NewReindexService(&fakeRunStore{}, products, &fakeQueue{}, logger.New("error"), defaultThreshold)
	if _, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: []string{id.New()}}); err == nil {
		t.Fatal("expected the catalog count error to propagate")
	}
}

// TestReindexService_Trigger_ScopeThreshold_SubstitutesFullScanAboveThreshold
// pins the core PR-1034 heuristic: a resolved scope covering more than
// fullScanThreshold of the catalog is enqueued as a full scan instead —
// visibly (ScopeParams still records what was requested), not silently.
func TestReindexService_Trigger_ScopeThreshold_SubstitutesFullScanAboveThreshold(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	// 21 of 100 products requested = 21% > the 20% default threshold.
	// Distinct real UUIDs, not repeated placeholders: a duplicate would
	// be deduplicated before the ratio is computed (see
	// TestReindexService_Trigger_ScopeProducts_DeduplicatesAndValidatesIDs),
	// silently shrinking this test's intended 21% below the threshold.
	ids := make([]string, 21)
	for i := range ids {
		ids[i] = id.New()
	}
	products := &fakeProductSource{products: make([]domainsearch.Product, 100)}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: ids})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	run := store.runs[runID]
	if run.Scope != "all" {
		t.Errorf("run.Scope = %q, want all (threshold substitution)", run.Scope)
	}
	if run.ScopeParams["requested_scope"] != "products" {
		t.Errorf("run.ScopeParams[requested_scope] = %v, want products (substitution must stay visible)", run.ScopeParams["requested_scope"])
	}

	job := queue.enqueued[0]
	if job.Payload["scope"] != "all" {
		t.Errorf("job payload scope = %v, want all", job.Payload["scope"])
	}
	if _, ok := job.Payload["product_ids"]; ok {
		t.Error("job payload should not carry product_ids once substituted to a full scan")
	}
}

// TestReindexService_Trigger_ScopeThreshold_KeepsScopedAtThreshold pins
// the other side of the boundary: exactly at the threshold (not above it)
// still runs scoped.
func TestReindexService_Trigger_ScopeThreshold_KeepsScopedAtThreshold(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	// 20 of 100 products requested = exactly 20%, not above it. Distinct
	// real UUIDs — see the same note in
	// ScopeThreshold_SubstitutesFullScanAboveThreshold above.
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = id.New()
	}
	products := &fakeProductSource{products: make([]domainsearch.Product, 100)}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: ids})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if store.runs[runID].Scope != "products" {
		t.Errorf("run.Scope = %q, want products (exactly at the threshold, not above it)", store.runs[runID].Scope)
	}
}

// TestReindexService_Trigger_ScopeThreshold_HardCapSubstitutesFullScan
// pins the absolute cap (maxScopedReindexProductIDs) that applies
// independent of the percentage threshold: an ID count over the cap
// substitutes a full scan without ever consulting the catalog size at
// all — proven, as in TestReindexService_Trigger_EmptyResolutionSkipsCountAll,
// by making CountAll itself fail and asserting Trigger still succeeds.
// On a real catalog this matters most exactly when it looks harmless by
// percentage (a huge catalog where even 10,001 ids is a small fraction) —
// the byte size of the job payload doesn't care about the fraction.
func TestReindexService_Trigger_ScopeThreshold_HardCapSubstitutesFullScan(t *testing.T) {
	store := &fakeRunStore{}
	queue := &fakeQueue{}
	const over = 10_001 // one past maxScopedReindexProductIDs
	ids := make([]string, over)
	for i := range ids {
		ids[i] = id.New()
	}
	products := &fakeProductSource{countErr: errors.New("CountAll must not be called once the hard cap alone decides the outcome")}
	svc, _ := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)

	runID, err := svc.Trigger(context.Background(), searchApp.ScopeProducts{IDs: ids})
	if err != nil {
		t.Fatalf("Trigger: %v (CountAll should not have been consulted once the hard cap tripped)", err)
	}
	if store.runs[runID].Scope != "all" {
		t.Errorf("run.Scope = %q, want all (hard cap exceeded)", store.runs[runID].Scope)
	}
	job := queue.enqueued[0]
	if _, ok := job.Payload["product_ids"]; ok {
		t.Error("job payload should not carry product_ids once substituted to a full scan")
	}
}
