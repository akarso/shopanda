package storefront_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

func TestFullPageCache_FPCMetrics_HitMissBypass(t *testing.T) {
	backend := newFPCMemCache()
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
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
	).WithFPCObserver(obs).WithFullPageCache(backend, time.Minute, true, nil)
	mux := http.NewServeMux()
	product := h.Product()
	mux.HandleFunc("GET /products/{slug}", product)
	mux.HandleFunc("POST /products/{slug}", product)

	miss := httptest.NewRecorder()
	mux.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if miss.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("first = %q, want MISS", miss.Header().Get("X-Shopanda-Cache"))
	}

	hit := httptest.NewRecorder()
	mux.ServeHTTP(hit, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if hit.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("second = %q, want HIT", hit.Header().Get("X-Shopanda-Cache"))
	}

	bypass := httptest.NewRecorder()
	mux.ServeHTTP(bypass, httptest.NewRequest(http.MethodPost, "/products/widget", nil))
	if bypass.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("POST header = %q, want BYPASS", bypass.Header().Get("X-Shopanda-Cache"))
	}

	snap := obs.Snapshot()
	var pdp cacheApp.FPCRouteSnapshot
	for _, r := range snap.Routes {
		if r.Route == cacheApp.RoutePDP {
			pdp = r
		}
	}
	if pdp.Misses != 1 || pdp.Hits != 1 || pdp.Bypasses != 1 {
		t.Fatalf("pdp counters = %+v, want miss=1 hit=1 bypass=1", pdp)
	}
	if pdp.HitRate < 0.49 || pdp.HitRate > 0.51 {
		t.Fatalf("hit_rate = %v, want ~0.5", pdp.HitRate)
	}
	if snap.PagesStored != 1 {
		t.Fatalf("pages_stored = %d, want 1", snap.PagesStored)
	}
}

func TestFullPageCache_SoftTTLStablePagesStoredAndNoPurge(t *testing.T) {
	backend := newFPCMemCache()
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
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
	).WithFPCObserver(obs).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("warm = %q", rec.Header().Get("X-Shopanda-Cache"))
	}
	if got := obs.Snapshot().PagesStored; got != 1 {
		t.Fatalf("after warm pages_stored = %d, want 1", got)
	}

	// Force soft-TTL expiry on the only key.
	for k := range backend.entries {
		var pe cacheApp.PageEntry
		_, _ = backend.Get(k, &pe)
		pe.StoredAt = time.Now().UTC().Add(-2 * time.Minute)
		pe.TTLNanos = int64(time.Minute)
		_ = backend.Set(k, pe, time.Minute)
	}

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			r := httptest.NewRecorder()
			router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
		}()
	}
	wg.Wait()

	snap := obs.Snapshot()
	if snap.Purges.TagInvalidation != 0 || snap.Purges.ManualURL != 0 {
		t.Fatalf("soft TTL must not emit purge counters: %+v", snap.Purges)
	}
	if snap.PagesStored != 1 {
		t.Fatalf("pages_stored = %d after soft-TTL refill, want 1 (evict once then store)", snap.PagesStored)
	}
}

// getFailCache wraps a working store but makes every Get fail — models a Redis
// blip where Set still works. Outer Get, stampede warm Get, and store probe
// Get can all fail on one miss; backend_get_errors must still be 1.
type getFailCache struct {
	*fpcMemCache
}

func (g *getFailCache) Get(string, any) (bool, error) {
	return false, errors.New("redis blip")
}

func TestFullPageCache_BackendGetErrorOncePerRequest(t *testing.T) {
	backend := &getFailCache{fpcMemCache: newFPCMemCache()}
	obs := cacheApp.NewFPCObserver(metrics.Noop())
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
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
	).WithFPCObserver(obs).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if got := rec.Header().Get("X-Shopanda-Cache"); got != "MISS" && got != "BYPASS" {
		t.Fatalf("header = %q", got)
	}
	snap := obs.Snapshot()
	if snap.BackendGetErrors != 1 {
		t.Fatalf("backend_get_errors = %d, want 1 (once per request despite multiple Get failures)", snap.BackendGetErrors)
	}
	// Probe Get failed → fail-closed wasAbsent=false → pages_stored stays 0
	// even though Set succeeded (key may have already existed).
	if snap.PagesStored != 0 {
		t.Fatalf("pages_stored = %d, want 0 when probe Get errors", snap.PagesStored)
	}
}
