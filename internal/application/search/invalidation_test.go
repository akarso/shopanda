package search_test

import (
	"context"
	"errors"
	"testing"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// fakeCategorySource is a minimal domainsearch.CategorySource fake for
// IndexUpdateSubscriber's category-event handler tests.
type fakeCategorySource struct {
	categories map[string]domainsearch.Category
	err        error
}

func (f *fakeCategorySource) GetByID(_ context.Context, categoryID string) (domainsearch.Category, bool, error) {
	if f.err != nil {
		return domainsearch.Category{}, false, f.err
	}
	c, ok := f.categories[categoryID]
	return c, ok, nil
}

// fakeCategoryIndexEngine wraps fakeSearchEngine (already defined in
// reindex_handler_test.go, same package) to also record IndexCategory/
// RemoveCategory calls for assertions.
type fakeCategoryIndexEngine struct {
	fakeSearchEngine
	indexedCategories []domainsearch.Category
	removedCategories []string
	indexCategoryErr  error
	removeCategoryErr error
}

func (f *fakeCategoryIndexEngine) IndexCategory(_ context.Context, c domainsearch.Category) error {
	if f.indexCategoryErr != nil {
		return f.indexCategoryErr
	}
	f.indexedCategories = append(f.indexedCategories, c)
	return nil
}

func (f *fakeCategoryIndexEngine) RemoveCategory(_ context.Context, categoryID string) error {
	if f.removeCategoryErr != nil {
		return f.removeCategoryErr
	}
	f.removedCategories = append(f.removedCategories, categoryID)
	return nil
}

func newIndexUpdateSubscriber(t *testing.T) (*searchApp.IndexUpdateSubscriber, *fakeQueue) {
	t.Helper()
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	return searchApp.NewIndexUpdateSubscriber(svc, &fakeSearchEngine{}, &fakeCategorySource{}, logger.New("error")), queue
}

func TestIndexUpdateSubscriber_HandleProductCreated_TriggersReindex(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductCreated, "test", catalog.ProductCreatedData{ProductID: id.New()})

	if err := sub.HandleProductCreated(context.Background(), evt); err != nil {
		t.Fatalf("HandleProductCreated: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_HandleProductUpdated_TriggersReindex(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: id.New()})

	if err := sub.HandleProductUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleProductUpdated: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
}

// TestIndexUpdateSubscriber_HandleProductUpdated_CategoryAssignmentReuse
// pins the deliberate reuse of catalog.EventProductUpdated for a
// category-assignment change (see PR-1036.md's "Design decisions") —
// HandleProductUpdated doesn't care why the event fired, only that a
// ProductID is present.
func TestIndexUpdateSubscriber_HandleProductUpdated_CategoryAssignmentReuse(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductUpdated, "category.assignment", catalog.ProductUpdatedData{ProductID: id.New()})

	if err := sub.HandleProductUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleProductUpdated: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_HandlePriceUpserted_TriggersReindex(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(pricing.EventPriceUpserted, "test", pricing.PriceUpsertedData{ProductID: id.New(), VariantID: "v1"})

	if err := sub.HandlePriceUpserted(context.Background(), evt); err != nil {
		t.Fatalf("HandlePriceUpserted: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_HandleStockUpdated_TriggersReindex(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(inventory.EventStockUpdated, "test", inventory.StockUpdatedData{ProductID: id.New(), VariantID: "v1", SKU: "sku-1", Quantity: 5})

	if err := sub.HandleStockUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleStockUpdated: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_WrongEventDataType_ReturnsErrorNoEnqueue(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductUpdated, "test", "not-the-right-type")

	if err := sub.HandleProductUpdated(context.Background(), evt); err == nil {
		t.Fatal("expected an error for unexpected event data type")
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued %d jobs, want 0", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_EmptyProductID_NoOp(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: ""})

	if err := sub.HandleProductUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleProductUpdated: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued %d jobs, want 0 for an empty product id", len(queue.enqueued))
	}
}

// TestIndexUpdateSubscriber_Debounce_CoalescesBurstIntoOneEnqueue pins
// PR-1036's debounce requirement: a burst of events for the same product
// within the debounce window enqueues at most one reindex.
func TestIndexUpdateSubscriber_Debounce_CoalescesBurstIntoOneEnqueue(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: id.New()})

	for i := 0; i < 5; i++ {
		if err := sub.HandleProductUpdated(context.Background(), evt); err != nil {
			t.Fatalf("HandleProductUpdated[%d]: %v", i, err)
		}
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs for a 5-event burst on one product, want 1", len(queue.enqueued))
	}
}

// TestIndexUpdateSubscriber_Debounce_DifferentProductsNotCoalesced pins
// that the debounce is keyed per product ID, not global.
func TestIndexUpdateSubscriber_Debounce_DifferentProductsNotCoalesced(t *testing.T) {
	sub, queue := newIndexUpdateSubscriber(t)
	evt1 := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: id.New()})
	evt2 := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: id.New()})

	if err := sub.HandleProductUpdated(context.Background(), evt1); err != nil {
		t.Fatalf("HandleProductUpdated(p1): %v", err)
	}
	if err := sub.HandleProductUpdated(context.Background(), evt2); err != nil {
		t.Fatalf("HandleProductUpdated(p2): %v", err)
	}
	if len(queue.enqueued) != 2 {
		t.Fatalf("enqueued %d jobs for two distinct products, want 2", len(queue.enqueued))
	}
}

// TestIndexUpdateSubscriber_TriggerFailure_EvictsDebounceEntry pins a
// fixed bug: allow() reserved the debounce slot before Trigger ran, so a
// failed Trigger (queue full, DB blip) left the slot reserved anyway —
// the next same-product event within the window was silently swallowed
// (scheduleReindex returns nil for a debounced call) even though no
// reindex had actually succeeded, leaving the index stale with no
// recovery path except an unrelated future edit or the next scheduled
// full reindex. A failed Trigger must instead release its reservation so
// the very next event for that product is genuinely retried, not
// falsely debounced.
func TestIndexUpdateSubscriber_TriggerFailure_EvictsDebounceEntry(t *testing.T) {
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{enqueueErr: errors.New("queue unavailable")}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := searchApp.NewIndexUpdateSubscriber(svc, &fakeSearchEngine{}, &fakeCategorySource{}, logger.New("error"))

	productID := id.New()
	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{ProductID: productID})

	if err := sub.HandleProductUpdated(context.Background(), evt); err == nil {
		t.Fatal("expected the first (failing) Trigger to return an error")
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs after the first attempt, want 1 (attempted, even though it failed)", len(queue.enqueued))
	}

	// A second event for the same product, still well within the debounce
	// window (no real time has passed) — pre-fix, this would be silently
	// debounced (nil error, no attempt); post-fix, the failed reservation
	// was evicted, so this must be genuinely retried (and fail again,
	// since the queue is still broken) rather than silently swallowed.
	if err := sub.HandleProductUpdated(context.Background(), evt); err == nil {
		t.Fatal("expected the second Trigger to be genuinely retried (and fail again), not silently debounced")
	}
	if len(queue.enqueued) != 2 {
		t.Fatalf("enqueued %d jobs after the second attempt, want 2 (genuinely retried, not debounced away)", len(queue.enqueued))
	}
}

func TestIndexUpdateSubscriber_Register_NilBusPanics(t *testing.T) {
	sub, _ := newIndexUpdateSubscriber(t)
	defer func() {
		if recover() == nil {
			t.Fatal("expected Register(nil) to panic")
		}
	}()
	sub.Register(nil)
}

func TestNewIndexUpdateSubscriber_NilDeps(t *testing.T) {
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected NewIndexUpdateSubscriber(nil, ...) to panic")
		}
	}()
	searchApp.NewIndexUpdateSubscriber(nil, &fakeSearchEngine{}, &fakeCategorySource{}, logger.New("error"))
	_ = svc
}

func TestNewIndexUpdateSubscriber_NilEnginePanics(t *testing.T) {
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected NewIndexUpdateSubscriber(svc, nil, ...) to panic")
		}
	}()
	searchApp.NewIndexUpdateSubscriber(svc, nil, &fakeCategorySource{}, logger.New("error"))
}

func TestNewIndexUpdateSubscriber_NilCategorySourcePanics(t *testing.T) {
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected NewIndexUpdateSubscriber(svc, engine, nil, ...) to panic")
		}
	}()
	searchApp.NewIndexUpdateSubscriber(svc, &fakeSearchEngine{}, nil, logger.New("error"))
}

func TestIndexUpdateSubscriber_HandleCategoryCreated_IndexesCategory(t *testing.T) {
	categoryID := id.New()
	engine := &fakeCategoryIndexEngine{}
	categories := &fakeCategorySource{categories: map[string]domainsearch.Category{
		categoryID: {ID: categoryID, Name: "Shoes", Slug: "shoes", ProductCount: 3},
	}}
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := searchApp.NewIndexUpdateSubscriber(svc, engine, categories, logger.New("error"))

	evt := event.New(catalog.EventCategoryCreated, "test", catalog.CategoryCreatedData{CategoryID: categoryID, Name: "Shoes", Slug: "shoes"})
	if err := sub.HandleCategoryCreated(context.Background(), evt); err != nil {
		t.Fatalf("HandleCategoryCreated: %v", err)
	}
	if len(engine.indexedCategories) != 1 || engine.indexedCategories[0].ID != categoryID {
		t.Fatalf("indexedCategories = %+v, want one entry for %s", engine.indexedCategories, categoryID)
	}
}

func TestIndexUpdateSubscriber_HandleCategoryUpdated_IndexesCategory(t *testing.T) {
	categoryID := id.New()
	engine := &fakeCategoryIndexEngine{}
	categories := &fakeCategorySource{categories: map[string]domainsearch.Category{
		categoryID: {ID: categoryID, Name: "Shoes v2", Slug: "shoes-v2"},
	}}
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := searchApp.NewIndexUpdateSubscriber(svc, engine, categories, logger.New("error"))

	evt := event.New(catalog.EventCategoryUpdated, "test", catalog.CategoryUpdatedData{CategoryID: categoryID, Name: "Shoes v2", Slug: "shoes-v2"})
	if err := sub.HandleCategoryUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleCategoryUpdated: %v", err)
	}
	if len(engine.indexedCategories) != 1 || engine.indexedCategories[0].Name != "Shoes v2" {
		t.Fatalf("indexedCategories = %+v, want one entry named Shoes v2", engine.indexedCategories)
	}
}

// TestIndexUpdateSubscriber_HandleCategoryUpdated_DeletedCategory_NoOp pins
// that a category deleted between the event firing and the handler
// running is not an error — CategorySource.GetByID's own doc comment.
func TestIndexUpdateSubscriber_HandleCategoryUpdated_DeletedCategory_NoOp(t *testing.T) {
	categoryID := id.New()
	engine := &fakeCategoryIndexEngine{}
	categories := &fakeCategorySource{}
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := searchApp.NewIndexUpdateSubscriber(svc, engine, categories, logger.New("error"))

	evt := event.New(catalog.EventCategoryUpdated, "test", catalog.CategoryUpdatedData{CategoryID: categoryID})
	if err := sub.HandleCategoryUpdated(context.Background(), evt); err != nil {
		t.Fatalf("HandleCategoryUpdated: %v", err)
	}
	if len(engine.indexedCategories) != 0 {
		t.Fatalf("indexedCategories = %+v, want none for a deleted category", engine.indexedCategories)
	}
}

func TestIndexUpdateSubscriber_HandleCategoryDeleted_RemovesCategory(t *testing.T) {
	categoryID := id.New()
	engine := &fakeCategoryIndexEngine{}
	store := &fakeRunStore{}
	products := &fakeProductSource{}
	queue := &fakeQueue{}
	svc, err := searchApp.NewReindexService(store, products, queue, logger.New("error"), defaultThreshold)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	sub := searchApp.NewIndexUpdateSubscriber(svc, engine, &fakeCategorySource{}, logger.New("error"))

	evt := event.New(catalog.EventCategoryDeleted, "test", catalog.CategoryDeletedData{CategoryID: categoryID})
	if err := sub.HandleCategoryDeleted(context.Background(), evt); err != nil {
		t.Fatalf("HandleCategoryDeleted: %v", err)
	}
	if len(engine.removedCategories) != 1 || engine.removedCategories[0] != categoryID {
		t.Fatalf("removedCategories = %v, want [%s]", engine.removedCategories, categoryID)
	}
}

func TestIndexUpdateSubscriber_HandleCategoryCreated_WrongEventDataType_ReturnsError(t *testing.T) {
	sub, _ := newIndexUpdateSubscriber(t)
	evt := event.New(catalog.EventCategoryCreated, "test", "not-the-right-type")

	if err := sub.HandleCategoryCreated(context.Background(), evt); err == nil {
		t.Fatal("expected an error for unexpected event data type")
	}
}
