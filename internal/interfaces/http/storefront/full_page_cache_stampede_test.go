package storefront_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

func TestFullPageCache_StampedeSingleRender(t *testing.T) {
	backend := newFPCMemCache()
	var renders atomic.Int32
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				renders.Add(1)
				time.Sleep(80 * time.Millisecond)
				return &catalog.Product{
					ID:          "p1",
					Name:        "Widget",
					Slug:        slug,
					Description: "A fine widget",
					Status:      catalog.StatusActive,
				}, nil
			},
		},
		catsByProduct: map[string][]string{"p1": {"c1"}},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	recs := make([]*httptest.ResponseRecorder, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
			recs[i] = rec
		}(i)
	}
	wg.Wait()

	if got := renders.Load(); got != 1 {
		t.Fatalf("product lookups = %d, want 1 (stampede coalesced)", got)
	}
	for i, rec := range recs {
		if rec.Code != http.StatusOK {
			t.Fatalf("req %d status = %d", i, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Widget") {
			t.Fatalf("req %d body missing product: %q", i, rec.Body.String())
		}
	}
}

func TestFullPageCache_StampedeLeaderRechecksWarmCache(t *testing.T) {
	backend := newFPCMemCache()
	var renders atomic.Int32
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				renders.Add(1)
				return &catalog.Product{
					ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget", Status: catalog.StatusActive,
				}, nil
			},
		},
		catsByProduct: map[string][]string{"p1": {"c1"}},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	missSeen := make(chan struct{})
	release := make(chan struct{})
	var blockFirst atomic.Bool
	blockFirst.Store(true)
	h.WithFPCAfterCacheMissForTest(func() {
		if blockFirst.CompareAndSwap(true, false) {
			close(missSeen)
			<-release
		}
	})
	t.Cleanup(func() { h.WithFPCAfterCacheMissForTest(nil) })

	var slowRec *httptest.ResponseRecorder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
		slowRec = rec
	}()

	select {
	case <-missSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first miss")
	}

	// Fill the key while the first request is paused between miss and Do.
	warm := httptest.NewRecorder()
	router.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if warm.Header().Get("X-Shopanda-Cache") != "MISS" && warm.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("warmer status header = %q", warm.Header().Get("X-Shopanda-Cache"))
	}
	if renders.Load() != 1 {
		t.Fatalf("warmer renders = %d, want 1", renders.Load())
	}

	close(release)
	wg.Wait()

	if got := renders.Load(); got != 1 {
		t.Fatalf("product lookups = %d, want 1 (leader rechecked warm cache)", got)
	}
	if slowRec.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("paused leader header = %q, want HIT", slowRec.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_StampedeWaiterRerendersWhenEntryPurged(t *testing.T) {
	backend := &purgeOnSetCache{fpcMemCache: newFPCMemCache()}
	var renders atomic.Int32
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				renders.Add(1)
				time.Sleep(60 * time.Millisecond)
				return &catalog.Product{
					ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget", Status: catalog.StatusActive,
				}, nil
			},
		},
		catsByProduct: map[string][]string{"p1": {"c1"}},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	recs := make([]*httptest.ResponseRecorder, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
			recs[i] = rec
		}(i)
	}
	wg.Wait()

	if got := renders.Load(); got < 2 {
		t.Fatalf("product lookups = %d, want >=2 (waiters re-render after purge)", got)
	}
	for i, rec := range recs {
		if rec.Code != http.StatusOK {
			t.Fatalf("req %d status = %d", i, rec.Code)
		}
		if rec.Header().Get("X-Shopanda-Cache") == "HIT" {
			t.Fatalf("req %d got HIT after purge revival; body=%q", i, rec.Body.String())
		}
	}
}

// purgeOnSetCache deletes each key immediately after SetWithTags so a
// stampede waiter never finds the leader's just-written entry.
type purgeOnSetCache struct {
	*fpcMemCache
}

func (c *purgeOnSetCache) SetWithTags(ctx context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	if err := c.fpcMemCache.SetWithTags(ctx, key, value, ttl, tags...); err != nil {
		return err
	}
	return c.Delete(key)
}
