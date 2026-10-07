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
