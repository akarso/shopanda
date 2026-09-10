package search

import (
	"context"
	"sync"
	"testing"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
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

type fakeInternalCategorySource struct{}

func (f *fakeInternalCategorySource) GetByID(context.Context, string) (domainsearch.Category, bool, error) {
	return domainsearch.Category{}, false, nil
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
	sub := NewIndexUpdateSubscriber(svc, &fakeInternalSearchEngine{}, &fakeInternalCategorySource{}, noopLogger{})
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
