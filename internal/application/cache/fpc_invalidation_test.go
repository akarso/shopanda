package cache_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
)

type tagMemCache struct {
	mu      sync.Mutex
	entries map[string]any
	tags    map[string]map[string]struct{} // tag → keys
}

func newTagMemCache() *tagMemCache {
	return &tagMemCache{
		entries: make(map[string]any),
		tags:    make(map[string]map[string]struct{}),
	}
}

func (m *tagMemCache) Get(key string, dest any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.entries[key]
	if !ok {
		return false, nil
	}
	if p, ok := dest.(*cacheApp.PageEntry); ok {
		if pe, ok := v.(cacheApp.PageEntry); ok {
			*p = pe
			return true, nil
		}
	}
	return true, nil
}
func (m *tagMemCache) Set(key string, value any, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = value
	return nil
}
func (m *tagMemCache) Incr(string, int64, time.Duration) (int64, error) { return 0, nil }
func (m *tagMemCache) CompareAndSubtract(string, int64) (int64, error)  { return 0, nil }
func (m *tagMemCache) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}
func (m *tagMemCache) DeleteByPrefix(context.Context, string) error { return nil }
func (m *tagMemCache) SetWithTags(_ context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	_ = m.Set(key, value, ttl)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tag := range cache.UniqueTags(tags) {
		if m.tags[tag] == nil {
			m.tags[tag] = make(map[string]struct{})
		}
		m.tags[tag][key] = struct{}{}
	}
	return nil
}
func (m *tagMemCache) DeleteByTag(_ context.Context, tag string) (int64, error) {
	tag = cache.NormalizeTag(tag)
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := m.tags[tag]
	delete(m.tags, tag)
	var n int64
	for k := range keys {
		if _, ok := m.entries[k]; ok {
			delete(m.entries, k)
			n++
		}
		for t, set := range m.tags {
			delete(set, k)
			if len(set) == 0 {
				delete(m.tags, t)
			}
		}
	}
	return n, nil
}
func (m *tagMemCache) Stats(context.Context) (cache.Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cache.Stats{Backend: "mem", Keys: int64(len(m.entries))}, nil
}
func (m *tagMemCache) FlushAll(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := int64(len(m.entries))
	m.entries = make(map[string]any)
	m.tags = make(map[string]map[string]struct{})
	return n, nil
}

func TestFPCInvalidation_ProductPricePurgesTaggedPagesOnly(t *testing.T) {
	backend := newTagMemCache()
	ctx := context.Background()
	pdp := cacheApp.Key(cacheApp.RoutePDP, "/products/widget", "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	plp := cacheApp.Key(cacheApp.RoutePLP, "/products", "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	other := cacheApp.Key(cacheApp.RoutePDP, "/products/other", "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, pdp, cacheApp.PageEntry{HTML: "pdp"}, time.Minute, cacheApp.ProductTag("p1"))
	_ = backend.SetWithTags(ctx, plp, cacheApp.PageEntry{HTML: "plp"}, time.Minute, cacheApp.ProductTag("p1"), cacheApp.ProductTag("p2"))
	_ = backend.SetWithTags(ctx, other, cacheApp.PageEntry{HTML: "other"}, time.Minute, cacheApp.ProductTag("p2"))

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{})
	if err := sub.HandlePriceUpserted(ctx, event.New(pricing.EventPriceUpserted, "test", pricing.PriceUpsertedData{ProductID: "p1"})); err != nil {
		t.Fatal(err)
	}

	if _, ok := backend.entries[pdp]; ok {
		t.Fatal("PDP tagged with p1 must be purged")
	}
	if _, ok := backend.entries[plp]; ok {
		t.Fatal("PLP tagged with p1 must be purged")
	}
	if _, ok := backend.entries[other]; !ok {
		t.Fatal("unrelated product page must remain")
	}
}

func TestFPCInvalidation_CategoryAndPageAndStock(t *testing.T) {
	backend := newTagMemCache()
	ctx := context.Background()
	catKey := cacheApp.Key(cacheApp.RouteCategory, "/categories/shoes", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	pageKey := cacheApp.Key(cacheApp.RouteCMS, "/pages/about", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, catKey, cacheApp.PageEntry{HTML: "c"}, time.Minute, cacheApp.CategoryTag("c1"))
	_ = backend.SetWithTags(ctx, pageKey, cacheApp.PageEntry{HTML: "p"}, time.Minute, cacheApp.PageTag("page-1"))

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{})
	if err := sub.HandleCategoryUpdated(ctx, event.New(catalog.EventCategoryUpdated, "test", catalog.CategoryUpdatedData{CategoryID: "c1"})); err != nil {
		t.Fatal(err)
	}
	if err := sub.HandlePageDeleted(ctx, event.New(cms.EventPageDeleted, "test", cms.PageDeletedData{PageID: "page-1"})); err != nil {
		t.Fatal(err)
	}
	if err := sub.HandleStockUpdated(ctx, event.New(inventory.EventStockUpdated, "test", inventory.StockUpdatedData{ProductID: "p9"})); err != nil {
		t.Fatal(err)
	}
	if len(backend.entries) != 0 {
		t.Fatalf("expected empty cache, got %#v", backend.entries)
	}
}

func TestPurgeURL_DeletesVaryKeysOnly(t *testing.T) {
	backend := newTagMemCache()
	svc := cacheApp.NewAdminService(backend, nil)
	stores := []cacheApp.StoreVary{{ID: "s1", Language: "en", Currency: "EUR"}}
	path := "/products/widget"
	route := cacheApp.RoutePDP
	guest := cacheApp.Key(route, path, "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	auth := cacheApp.Key(route, path, "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthAuthenticated})
	other := cacheApp.Key(route, "/products/other", "", cacheApp.Vary{Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest})
	_ = backend.Set(guest, cacheApp.PageEntry{HTML: "g"}, time.Minute)
	_ = backend.Set(auth, cacheApp.PageEntry{HTML: "a"}, time.Minute)
	_ = backend.Set(other, cacheApp.PageEntry{HTML: "o"}, time.Minute)

	res, err := svc.PurgeURL(context.Background(), path, stores)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", res.Deleted)
	}
	if _, ok := backend.entries[guest]; ok {
		t.Fatal("guest key must be deleted")
	}
	if _, ok := backend.entries[auth]; ok {
		t.Fatal("auth key must be deleted")
	}
	if _, ok := backend.entries[other]; !ok {
		t.Fatal("other path must remain")
	}
}

func TestPurgeURLKeys_KeepsAttrQuery(t *testing.T) {
	path, keys, err := cacheApp.PurgeURLKeys("/products?page=2&attr_color=red&utm_source=x", []cacheApp.StoreVary{{ID: "s1", Language: "en", Currency: "EUR"}})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/products" {
		t.Fatalf("path = %q", path)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %d, want 2", len(keys))
	}
	want := cacheApp.Key(cacheApp.RoutePLP, "/products", "page=2&attr_color=red&utm_source=x", cacheApp.Vary{
		Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest,
	}, "attr_color")
	found := false
	for _, k := range keys {
		if k == want {
			found = true
		}
		if strings.Contains(k, "utm_source") {
			t.Fatalf("tracking param leaked into key: %q", k)
		}
	}
	if !found {
		t.Fatalf("missing attr_/page key among %#v (want %q)", keys, want)
	}
}

func TestPurgeURLKeys_ExpandsQueryLang(t *testing.T) {
	_, keys, err := cacheApp.PurgeURLKeys("/products/widget?lang=de", []cacheApp.StoreVary{{ID: "s1", Language: "en", Currency: "EUR"}})
	if err != nil {
		t.Fatal(err)
	}
	wantDE := cacheApp.Key(cacheApp.RoutePDP, "/products/widget", "", cacheApp.Vary{
		Store: "s1", Language: "de", Currency: "EUR", AuthState: cacheApp.AuthGuest,
	})
	wantEN := cacheApp.Key(cacheApp.RoutePDP, "/products/widget", "", cacheApp.Vary{
		Store: "s1", Language: "en", Currency: "EUR", AuthState: cacheApp.AuthGuest,
	})
	var haveDE, haveEN bool
	for _, k := range keys {
		if k == wantDE {
			haveDE = true
		}
		if k == wantEN {
			haveEN = true
		}
	}
	if !haveDE || !haveEN {
		t.Fatalf("want both en and de guest keys among %#v", keys)
	}
}

func TestFPCInvalidation_ProductCreatedPurgesListingTag(t *testing.T) {
	backend := newTagMemCache()
	ctx := context.Background()
	plp := cacheApp.Key(cacheApp.RoutePLP, "/products", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, plp, cacheApp.PageEntry{HTML: "plp"}, time.Minute, cacheApp.ListingTag())

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{})
	if err := sub.HandleProductCreated(ctx, event.New(catalog.EventProductCreated, "test", catalog.ProductCreatedData{ProductID: "brand-new"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.entries[plp]; ok {
		t.Fatal("listing shell must be purged on product create")
	}
}

func TestFPCInvalidation_CategoryCreatedPurgesNavigationTag(t *testing.T) {
	backend := newTagMemCache()
	ctx := context.Background()
	home := cacheApp.Key(cacheApp.RouteHome, "/", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, home, cacheApp.PageEntry{HTML: "home"}, time.Minute, cacheApp.NavigationTag())

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{})
	if err := sub.HandleCategoryCreated(ctx, event.New(catalog.EventCategoryCreated, "test", catalog.CategoryCreatedData{CategoryID: "new-cat"})); err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.entries[home]; ok {
		t.Fatal("navigation shell must be purged on category create")
	}
}

func TestFPCInvalidation_AfterProductsIndexed(t *testing.T) {
	backend := newTagMemCache()
	ctx := context.Background()
	plp := cacheApp.Key(cacheApp.RoutePLP, "/products", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	pdp := cacheApp.Key(cacheApp.RoutePDP, "/products/widget", "", cacheApp.Vary{AuthState: cacheApp.AuthGuest})
	_ = backend.SetWithTags(ctx, plp, cacheApp.PageEntry{HTML: "plp"}, time.Minute, cacheApp.ListingTag())
	_ = backend.SetWithTags(ctx, pdp, cacheApp.PageEntry{HTML: "pdp"}, time.Minute, cacheApp.ProductTag("p1"))

	sub := cacheApp.NewFPCInvalidationSubscriber(backend, &mockLogger{})
	if err := sub.AfterProductsIndexed(ctx, []string{"p1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.entries[plp]; ok {
		t.Fatal("listing must be purged after index")
	}
	if _, ok := backend.entries[pdp]; ok {
		t.Fatal("product page must be purged after index")
	}
}
