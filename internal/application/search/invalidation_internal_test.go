package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// noopLogger discards everything — only used where a test needs a real
// Logger to construct a ReindexService, not to assert on log output.
type noopLogger struct{}

func (noopLogger) Info(string, map[string]interface{})         {}
func (noopLogger) Error(string, error, map[string]interface{}) {}

// Minimal fakes for ReindexService's own dependencies — duplicated here
// (rather than reused from invalidation_test.go's fakeRunStore/fakeQueue/
// fakeProductSource) because those live in the external search_test
// package and aren't visible from this whitebox (package search) test
// file, which needs direct field access to shrink IndexUpdateSubscriber's
// unexported window for a fast trailing-flush test.

type fakeInternalRunStore struct{}

func (f *fakeInternalRunStore) Create(context.Context, domainsearch.Run) error { return nil }
func (f *fakeInternalRunStore) Get(context.Context, string) (*domainsearch.Run, error) {
	return nil, nil
}
func (f *fakeInternalRunStore) UpdateProgress(context.Context, string, int, int, int) error {
	return nil
}
func (f *fakeInternalRunStore) Finish(context.Context, string, domainsearch.RunStatus, string) error {
	return nil
}
func (f *fakeInternalRunStore) FindStaleProcessing(context.Context, time.Time, int) ([]domainsearch.Run, error) {
	return nil, nil
}

type fakeInternalProductSource struct{}

func (f *fakeInternalProductSource) CountAll(context.Context) (int, error) { return 0, nil }
func (f *fakeInternalProductSource) ListAll(context.Context, int, int) ([]domainsearch.Product, error) {
	return nil, nil
}
func (f *fakeInternalProductSource) ListByIDs(context.Context, []string) ([]domainsearch.Product, error) {
	return nil, nil
}
func (f *fakeInternalProductSource) ProductIDsByCategory(context.Context, []string) ([]string, error) {
	return nil, nil
}
func (f *fakeInternalProductSource) ProductIDsUpdatedSince(context.Context, time.Time) ([]string, error) {
	return nil, nil
}

// fakeInternalQueue counts Enqueue calls under a mutex — the trailing
// flush fires from its own goroutine (time.AfterFunc), so the test's
// assertion goroutine needs a safe way to observe it.
type fakeInternalQueue struct {
	mu    sync.Mutex
	count int
}

func (f *fakeInternalQueue) Enqueue(context.Context, domainjobs.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	return nil
}
func (f *fakeInternalQueue) Dequeue(context.Context) (*domainjobs.Job, error) { return nil, nil }
func (f *fakeInternalQueue) Complete(context.Context, string) error           { return nil }
func (f *fakeInternalQueue) Fail(context.Context, string, error) error        { return nil }

func (f *fakeInternalQueue) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

type fakeInternalSearchEngine struct{}

func (f *fakeInternalSearchEngine) Name() string { return "fake" }
func (f *fakeInternalSearchEngine) IndexProduct(context.Context, domainsearch.Product) error {
	return nil
}
func (f *fakeInternalSearchEngine) RemoveProduct(context.Context, string) error { return nil }
func (f *fakeInternalSearchEngine) IndexCategory(context.Context, domainsearch.Category) error {
	return nil
}
func (f *fakeInternalSearchEngine) RemoveCategory(context.Context, string) error { return nil }
func (f *fakeInternalSearchEngine) Search(context.Context, domainsearch.SearchQuery) (domainsearch.SearchResult, error) {
	return domainsearch.SearchResult{}, nil
}
func (f *fakeInternalSearchEngine) Suggest(context.Context, string, int) ([]domainsearch.Suggestion, error) {
	return nil, nil
}

type fakeInternalCategorySource struct {
	category domainsearch.Category
	found    bool
}

func (f *fakeInternalCategorySource) GetByID(context.Context, string) (domainsearch.Category, bool, error) {
	return f.category, f.found, nil
}

// fakeInternalCategoryLock is an in-process domainsearch.CategoryLock fake
// — real per-key mutual exclusion (same shape as the sync.Map-based lock
// this type replaced), enough to verify IndexUpdateSubscriber holds the
// lock for the right duration; it does not itself prove cross-instance
// behavior (that's AdvisoryLock's own integration test), only that the
// subscriber uses the CategoryLock port correctly.
type fakeInternalCategoryLock struct {
	locks sync.Map // map[string]*sync.Mutex
}

func (f *fakeInternalCategoryLock) Lock(_ context.Context, key string) (func() error, error) {
	muIface, _ := f.locks.LoadOrStore(key, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	return func() error {
		mu.Unlock()
		return nil
	}, nil
}

// fakeInternalRetryEngine wraps fakeInternalSearchEngine to fail
// IndexCategory a configurable number of times before succeeding, for
// TestIndexUpdateSubscriber_HandleCategoryCreated_RetriesTransientFailure.
type fakeInternalRetryEngine struct {
	fakeInternalSearchEngine
	failAttempts      int
	calls             int
	indexedCategories []domainsearch.Category
}

func (f *fakeInternalRetryEngine) IndexCategory(_ context.Context, c domainsearch.Category) error {
	f.calls++
	if f.calls <= f.failAttempts {
		return fmt.Errorf("transient failure (attempt %d)", f.calls)
	}
	f.indexedCategories = append(f.indexedCategories, c)
	return nil
}

// TestIndexUpdateSubscriber_Allow_DebounceWindow exercises allow directly
// (package-internal test) with fabricated timestamps instead of real
// sleeps, so the debounce window's exact boundary behavior is verified
// deterministically and fast.
func TestIndexUpdateSubscriber_Allow_DebounceWindow(t *testing.T) {
	s := &IndexUpdateSubscriber{window: productReindexDebounceWindow, lastFire: make(map[string]time.Time)}
	base := time.Now()

	if !s.allow("p1", base) {
		t.Fatal("first call for a product should be allowed")
	}
	if s.allow("p1", base.Add(time.Second)) {
		t.Fatal("a second call within the debounce window should be debounced")
	}
	if s.allow("p1", base.Add(productReindexDebounceWindow-time.Millisecond)) {
		t.Fatal("a call just under the window boundary should still be debounced")
	}
	if !s.allow("p1", base.Add(productReindexDebounceWindow+time.Millisecond)) {
		t.Fatal("a call just past the window boundary should be allowed again")
	}
}

func TestIndexUpdateSubscriber_Allow_PerProductKey(t *testing.T) {
	s := &IndexUpdateSubscriber{window: productReindexDebounceWindow, lastFire: make(map[string]time.Time)}
	base := time.Now()

	if !s.allow("p1", base) {
		t.Fatal("p1 should be allowed")
	}
	if !s.allow("p2", base) {
		t.Fatal("p2 should be allowed independently of p1's debounce")
	}
	if s.allow("p1", base.Add(time.Millisecond)) {
		t.Fatal("p1 should still be debounced")
	}
}

// TestIndexUpdateSubscriber_Allow_PrunesStaleEntries pins that lastFire
// doesn't grow unboundedly: an entry older than the debounce window is
// dropped on any later call, not just on reads of that same key.
func TestIndexUpdateSubscriber_Allow_PrunesStaleEntries(t *testing.T) {
	s := &IndexUpdateSubscriber{window: productReindexDebounceWindow, lastFire: make(map[string]time.Time)}
	base := time.Now()

	s.allow("stale", base)
	if len(s.lastFire) != 1 {
		t.Fatalf("lastFire len = %d, want 1", len(s.lastFire))
	}

	s.allow("other", base.Add(productReindexDebounceWindow+time.Millisecond))

	if _, ok := s.lastFire["stale"]; ok {
		t.Error("expected the stale entry to have been pruned")
	}
	if _, ok := s.lastFire["other"]; !ok {
		t.Error("expected the fresh entry to still be present")
	}
}

// TestIndexUpdateSubscriber_TrailingFlush_CatchesLaterMutationAfterEarlyCompletion
// pins a fixed bug: the debounce was leading-edge only — it fired once on
// the first event, then silently dropped every subsequent same-product
// event within the window. In production, a single-product reindex job
// typically finishes well before the multi-second debounce window ends,
// so a second real edit arriving during that window had no future event
// left to "catch up" on — the index would keep showing the first edit's
// state indefinitely, until an unrelated later edit or a scheduled full
// reindex. A trailing flush must fire once the window elapses so the
// product's actual latest state is always reindexed, not just its first
// mutation. Uses a shrunk window (not the real productReindexDebounceWindow)
// so this test observes the trailing timer actually firing without a
// multi-second sleep.
func TestIndexUpdateSubscriber_TrailingFlush_CatchesLaterMutationAfterEarlyCompletion(t *testing.T) {
	queue := &fakeInternalQueue{}
	svc, err := NewReindexService(&fakeInternalRunStore{}, &fakeInternalProductSource{}, queue, noopLogger{}, 1.0)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := NewIndexUpdateSubscriber(svc, &fakeInternalSearchEngine{}, &fakeInternalCategorySource{}, &fakeInternalCategoryLock{}, noopLogger{})
	sub.window = 40 * time.Millisecond

	productID := id.New()
	ctx := context.Background()

	// Leading edge: first mutation fires immediately — simulates the
	// reindex job for it completing well before the debounce window ends.
	if err := sub.scheduleReindex(ctx, productID); err != nil {
		t.Fatalf("first scheduleReindex: %v", err)
	}
	if got := queue.Count(); got != 1 {
		t.Fatalf("enqueued %d jobs after the first mutation, want 1", got)
	}

	// A second mutation arrives while still within the window. Pre-fix,
	// this was silently dropped with nothing left to ever pick it up.
	if err := sub.scheduleReindex(ctx, productID); err != nil {
		t.Fatalf("second scheduleReindex: %v", err)
	}
	if got := queue.Count(); got != 1 {
		t.Fatalf("enqueued %d jobs right after the second mutation, want still 1 (debounced, trailing flush not due yet)", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && queue.Count() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := queue.Count(); got != 2 {
		t.Fatalf("enqueued %d jobs after waiting past the window, want 2 (leading-edge + trailing flush) — the second mutation's effect must not be lost", got)
	}
}

// TestIndexUpdateSubscriber_RetryCategoryEngineCall_SucceedsAfterTransientFailures
// pins the CR fix for PR-1037: category-document indexing runs as a bare
// async event handler with no queue/retry behind it (unlike product
// reindexing), so without an in-process retry, a single transient engine
// error (a brief Meilisearch blip) would permanently drop that update —
// event.Bus.Publish only logs and drops async handler errors, it never
// retries them itself. retryCategoryEngineCall must succeed once the
// underlying call stops failing, without exhausting its attempt budget.
func TestIndexUpdateSubscriber_RetryCategoryEngineCall_SucceedsAfterTransientFailures(t *testing.T) {
	s := &IndexUpdateSubscriber{retryDelay: time.Millisecond}
	calls := 0
	err := s.retryCategoryEngineCall(context.Background(), func() error {
		calls++
		if calls < categoryIndexMaxAttempts {
			return errors.New("transient failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryCategoryEngineCall: %v", err)
	}
	if calls != categoryIndexMaxAttempts {
		t.Fatalf("calls = %d, want %d (succeeds on the last allowed attempt)", calls, categoryIndexMaxAttempts)
	}
}

// TestIndexUpdateSubscriber_RetryCategoryEngineCall_GivesUpAfterMaxAttempts
// pins the other half: a persistent failure must not retry forever — it
// gives up after categoryIndexMaxAttempts and returns the last error, same
// as today's (pre-fix) single-attempt behavior for a truly broken engine.
func TestIndexUpdateSubscriber_RetryCategoryEngineCall_GivesUpAfterMaxAttempts(t *testing.T) {
	s := &IndexUpdateSubscriber{retryDelay: time.Millisecond}
	calls := 0
	wantErr := errors.New("permanent failure")
	err := s.retryCategoryEngineCall(context.Background(), func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if calls != categoryIndexMaxAttempts {
		t.Fatalf("calls = %d, want exactly %d (no more, no fewer)", calls, categoryIndexMaxAttempts)
	}
}

// TestIndexUpdateSubscriber_RetryCategoryEngineCall_AbortsOnContextCancel
// pins that a retry loop doesn't outlive the bus's shutdown context —
// OnAsync handlers run with shutdownCtx, which Drain cancels after its
// grace window, and a retry loop that ignored cancellation could hold up
// process shutdown.
func TestIndexUpdateSubscriber_RetryCategoryEngineCall_AbortsOnContextCancel(t *testing.T) {
	s := &IndexUpdateSubscriber{retryDelay: time.Hour} // long enough that only cancellation ends the wait
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- s.retryCategoryEngineCall(ctx, func() error {
			calls++
			return errors.New("still failing")
		})
	}()
	// Let the first attempt run and enter its post-failure wait, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the last attempt's error, not nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retryCategoryEngineCall did not return after context cancellation")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cancelled during the wait before a second attempt)", calls)
	}
}

// TestIndexUpdateSubscriber_HandleCategoryCreated_RetriesTransientFailure
// exercises the retry wiring end-to-end through the public handler (not
// just retryCategoryEngineCall in isolation): HandleCategoryCreated must
// still report success once the engine's transient failures clear within
// the attempt budget, proving indexCategory actually routes IndexCategory
// calls through the retry helper rather than calling the engine directly.
func TestIndexUpdateSubscriber_HandleCategoryCreated_RetriesTransientFailure(t *testing.T) {
	engine := &fakeInternalRetryEngine{failAttempts: categoryIndexMaxAttempts - 1}
	categoryID := id.New()
	categories := &fakeInternalCategorySource{
		category: domainsearch.Category{ID: categoryID, Name: "Shoes", Slug: "shoes"},
		found:    true,
	}
	s := &IndexUpdateSubscriber{
		engine:       engine,
		categories:   categories,
		categoryLock: &fakeInternalCategoryLock{},
		log:          noopLogger{},
		retryDelay:   time.Millisecond,
	}

	evt := event.New(catalog.EventCategoryCreated, "test", catalog.CategoryCreatedData{CategoryID: categoryID, Name: "Shoes", Slug: "shoes"})
	if err := s.HandleCategoryCreated(context.Background(), evt); err != nil {
		t.Fatalf("HandleCategoryCreated: %v", err)
	}
	if engine.calls != categoryIndexMaxAttempts {
		t.Fatalf("engine.calls = %d, want %d (retried through the transient failures)", engine.calls, categoryIndexMaxAttempts)
	}
	if len(engine.indexedCategories) != 1 || engine.indexedCategories[0].ID != categoryID {
		t.Fatalf("indexedCategories = %+v, want one entry for %s", engine.indexedCategories, categoryID)
	}
}

// fakeInternalBlockingEngine lets a test pause IndexCategory mid-flight
// (after it starts, before it returns) to deterministically force the
// Update/Delete race lockCategory guards against — see
// TestIndexUpdateSubscriber_HandleCategoryUpdatedAndDeleted_SerializedNoResurrection.
type fakeInternalBlockingEngine struct {
	fakeInternalSearchEngine
	indexStarted chan struct{}
	proceedIndex chan struct{}

	mu                sync.Mutex
	indexedCategories []domainsearch.Category
	removedCategories []string
	callOrder         []string
}

func (f *fakeInternalBlockingEngine) IndexCategory(_ context.Context, c domainsearch.Category) error {
	close(f.indexStarted)
	<-f.proceedIndex
	f.mu.Lock()
	f.indexedCategories = append(f.indexedCategories, c)
	f.callOrder = append(f.callOrder, "index")
	f.mu.Unlock()
	return nil
}

func (f *fakeInternalBlockingEngine) RemoveCategory(_ context.Context, categoryID string) error {
	f.mu.Lock()
	f.removedCategories = append(f.removedCategories, categoryID)
	f.callOrder = append(f.callOrder, "remove")
	f.mu.Unlock()
	return nil
}

// TestIndexUpdateSubscriber_HandleCategoryUpdatedAndDeleted_SerializedNoResurrection
// pins the CR fix: an Update and a Delete for the same category are
// published from two independent HTTP requests, each dispatched to its
// own async goroutine with no ordering guarantee between them. Before the
// fix, an Update handler whose GetByID read ran before a concurrent
// Delete actually removed the category could still push that now-stale
// data to IndexCategory *after* the Delete handler's RemoveCategory
// already ran — resurrecting a deleted category as searchable,
// permanently (nothing else would ever remove it again). This forces
// Update to start first and pauses it mid-IndexCategory (still holding
// categoryID's lock), starts Delete concurrently, and asserts Delete
// cannot complete until Update's IndexCategory finishes and releases the
// lock — so the two engine calls are always fully serialized, and
// RemoveCategory (Delete) never lands before IndexCategory (Update) for
// this pair, only after.
func TestIndexUpdateSubscriber_HandleCategoryUpdatedAndDeleted_SerializedNoResurrection(t *testing.T) {
	categoryID := id.New()
	engine := &fakeInternalBlockingEngine{
		indexStarted: make(chan struct{}),
		proceedIndex: make(chan struct{}),
	}
	categories := &fakeInternalCategorySource{
		category: domainsearch.Category{ID: categoryID, Name: "Shoes", Slug: "shoes"},
		found:    true,
	}
	s := &IndexUpdateSubscriber{engine: engine, categories: categories, categoryLock: &fakeInternalCategoryLock{}, log: noopLogger{}, retryDelay: time.Millisecond}

	updateEvt := event.New(catalog.EventCategoryUpdated, "test", catalog.CategoryUpdatedData{CategoryID: categoryID, Name: "Shoes", Slug: "shoes"})
	deleteEvt := event.New(catalog.EventCategoryDeleted, "test", catalog.CategoryDeletedData{CategoryID: categoryID, Slug: "shoes"})

	updateDone := make(chan error, 1)
	go func() {
		updateDone <- s.HandleCategoryUpdated(context.Background(), updateEvt)
	}()

	select {
	case <-engine.indexStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleCategoryUpdated did not reach IndexCategory in time")
	}

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- s.HandleCategoryDeleted(context.Background(), deleteEvt)
	}()

	select {
	case <-deleteDone:
		t.Fatal("HandleCategoryDeleted completed before HandleCategoryUpdated released the category lock — the two must be serialized, not run concurrently")
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocked waiting for the lock.
	}

	close(engine.proceedIndex)

	if err := <-updateDone; err != nil {
		t.Fatalf("HandleCategoryUpdated: %v", err)
	}
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("HandleCategoryDeleted: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleCategoryDeleted did not complete after the lock was released")
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.callOrder) != 2 || engine.callOrder[0] != "index" || engine.callOrder[1] != "remove" {
		t.Fatalf("callOrder = %v, want [index remove] — RemoveCategory must never land before IndexCategory for this racing pair, or the deleted category would end up resurrected as searchable", engine.callOrder)
	}
}
