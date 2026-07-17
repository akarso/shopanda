package pimdemo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	sdkgraphql "github.com/akarso/shopanda/pkg/integrationsdk/graphql"
)

func mockPIMServer(t *testing.T, body string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestEnrichmentStep_FetchesAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := mockPIMServer(t, `{"data":{"product":{"marketing_title":"PIM Title","marketing_description":"PIM Desc","specs":[{"key":"material","value":"cotton"}]}}}`, &calls)
	t.Cleanup(srv.Close)

	client, err := sdkgraphql.New(sdkgraphql.Config{Endpoint: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	step := NewEnrichmentStep(newEnrichmentFetcher(client), newTTLCache(time.Minute), nil)

	product, err := catalog.NewProduct("p1", "Shirt", "shirt")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	ctx := composition.NewProductContext(&product)
	ctx.Ctx = context.Background()

	if err := step.Apply(ctx); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls after first = %d", calls.Load())
	}
	if len(ctx.Blocks) != 1 || ctx.Blocks[0].Type != BlockType {
		t.Fatalf("blocks = %+v", ctx.Blocks)
	}
	if ctx.Blocks[0].Data["cached"] != false {
		t.Fatalf("cached flag = %v", ctx.Blocks[0].Data["cached"])
	}

	ctx2 := composition.NewProductContext(&product)
	ctx2.Ctx = context.Background()
	if err := step.Apply(ctx2); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls after cache hit = %d", calls.Load())
	}
	if ctx2.Blocks[0].Data["cached"] != true {
		t.Fatalf("cached flag = %v", ctx2.Blocks[0].Data["cached"])
	}
}

func TestEnrichmentStep_GraphQLErrorDoesNotFailPipeline(t *testing.T) {
	srv := mockPIMServer(t, `{"errors":[{"message":"not found"}]}`, nil)
	t.Cleanup(srv.Close)

	client, err := sdkgraphql.New(sdkgraphql.Config{Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	step := NewEnrichmentStep(newEnrichmentFetcher(client), newTTLCache(time.Minute), nil)
	product, err := catalog.NewProduct("p1", "Shirt", "shirt")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	ctx := composition.NewProductContext(&product)
	ctx.Ctx = context.Background()
	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Fatalf("blocks = %+v", ctx.Blocks)
	}
}

func TestTTLCache_ExpiresEntries(t *testing.T) {
	cache := newTTLCache(20 * time.Millisecond)
	cache.Set("shirt", EnrichmentData{MarketingTitle: "Old"})
	time.Sleep(30 * time.Millisecond)
	if _, ok := cache.Get("shirt"); ok {
		t.Fatal("expected expired cache miss")
	}
}

func TestTTLCache_EvictsWhenAtCapacity(t *testing.T) {
	cache := newTTLCache(time.Minute)
	cache.maxEntries = 2
	cache.Set("one", EnrichmentData{MarketingTitle: "1"})
	cache.Set("two", EnrichmentData{MarketingTitle: "2"})
	cache.Set("three", EnrichmentData{MarketingTitle: "3"})

	if _, ok := cache.Get("one"); ok {
		t.Fatal("expected oldest entry to be evicted")
	}
	if _, ok := cache.Get("two"); !ok {
		t.Fatal("expected entry two to remain")
	}
	if _, ok := cache.Get("three"); !ok {
		t.Fatal("expected entry three to remain")
	}
}
