package storefront_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cacheapp "github.com/akarso/shopanda/internal/application/cache"
	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/application/composition"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth"
)

type fpcMemCache struct {
	entries map[string][]byte
	tags    map[string][]string
}

func newFPCMemCache() *fpcMemCache {
	return &fpcMemCache{entries: make(map[string][]byte), tags: make(map[string][]string)}
}

func (m *fpcMemCache) Get(key string, dest any) (bool, error) {
	raw, ok := m.entries[key]
	if !ok {
		return false, nil
	}
	if dest == nil {
		return true, nil
	}
	return true, json.Unmarshal(raw, dest)
}

func (m *fpcMemCache) Set(key string, value any, _ time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.entries[key] = b
	return nil
}
func (m *fpcMemCache) Incr(string, int64, time.Duration) (int64, error) { return 0, nil }
func (m *fpcMemCache) CompareAndSubtract(string, int64) (int64, error)  { return 0, nil }
func (m *fpcMemCache) Delete(key string) error                          { delete(m.entries, key); return nil }
func (m *fpcMemCache) DeleteByPrefix(context.Context, string) error     { return nil }
func (m *fpcMemCache) SetWithTags(_ context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	_ = ttl
	if err := m.Set(key, value, 0); err != nil {
		return err
	}
	m.tags[key] = append([]string(nil), tags...)
	return nil
}
func (m *fpcMemCache) DeleteByTag(context.Context, string) (int64, error) { return 0, nil }
func (m *fpcMemCache) Stats(context.Context) (cache.Stats, error) {
	return cache.Stats{Backend: "memory", Keys: int64(len(m.entries))}, nil
}
func (m *fpcMemCache) FlushAll(context.Context) (int64, error) {
	n := int64(len(m.entries))
	m.entries = make(map[string][]byte)
	m.tags = make(map[string][]string)
	return n, nil
}

type fpcProductRepo struct {
	mockStorefrontRepo
	catsByProduct map[string][]string
	listCalls     int
}

func (r *fpcProductRepo) ListCategoryIDsByProduct(_ context.Context, productID string) ([]string, error) {
	r.listCalls++
	if r.catsByProduct == nil {
		return nil, nil
	}
	return append([]string(nil), r.catsByProduct[productID]...), nil
}

func fpcHandler(t *testing.T, backend cache.Cache) *storefront.StorefrontHandler {
	t.Helper()
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				if slug == "missing" {
					return nil, nil
				}
				if slug == "boom" {
					return nil, errors.New("db down")
				}
				return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget"}, nil
			},
		},
		catsByProduct: map[string][]string{"p1": {"c1"}},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	return h.WithFullPageCache(backend, time.Minute, true, nil)
}

func TestFullPageCache_MissThenHit(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("miss status = %d", rec1.Code)
	}
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("first request header = %q, want MISS", rec1.Header().Get("X-Shopanda-Cache"))
	}
	if cc := rec1.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "public, max-age=") {
		t.Fatalf("miss Cache-Control = %q, want public, max-age=…", cc)
	}
	if len(backend.entries) != 1 {
		t.Fatalf("stored entries = %d, want 1", len(backend.entries))
	}
	var tagged bool
	for _, tags := range backend.tags {
		for _, tag := range tags {
			if tag == "product:p1" {
				tagged = true
			}
		}
	}
	if !tagged {
		t.Fatalf("missing product tag: %#v", backend.tags)
	}
	var catTagged bool
	for _, tags := range backend.tags {
		for _, tag := range tags {
			if tag == "category:c1" {
				catTagged = true
			}
		}
	}
	if !catTagged {
		t.Fatalf("missing PDP category tag: %#v", backend.tags)
	}

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("second request header = %q, want HIT", rec2.Header().Get("X-Shopanda-Cache"))
	}
	if cc := rec2.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "public, max-age=") {
		t.Fatalf("hit Cache-Control = %q, want public, max-age=…", cc)
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Fatal("HIT body must match MISS body")
	}
}

func TestFullPageCache_CartNeverStored(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cart", nil))
	if len(backend.entries) != 0 {
		t.Fatalf("cart must not write FPC entries, got %d", len(backend.entries))
	}
}

func TestFullPageCache_VaryByStoreAndAuth(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	s1, err := store.NewStore("s1", "one", "One", "EUR", "DE", "en", "one.example")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := store.NewStore("s2", "two", "Two", "USD", "US", "en", "two.example")
	if err != nil {
		t.Fatal(err)
	}

	req1 := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req1 = req1.WithContext(store.WithStore(req1.Context(), &s1))
	router.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req2 = req2.WithContext(store.WithStore(req2.Context(), &s2))
	router.ServeHTTP(httptest.NewRecorder(), req2)

	id, err := identity.NewIdentity("cust-a", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	req3 := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req3 = req3.WithContext(store.WithStore(auth.WithIdentity(req3.Context(), id), &s1))
	router.ServeHTTP(httptest.NewRecorder(), req3)

	if len(backend.entries) != 3 {
		t.Fatalf("entries = %d, want 3 (store×2 + auth)", len(backend.entries))
	}
}

func TestFullPageCache_GuestsShareEntryWithoutCustomerData(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	reqA := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	reqA.Header.Set("Cookie", "shopanda_csrf=token-alice")
	recA := httptest.NewRecorder()
	router.ServeHTTP(recA, reqA)

	reqB := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	reqB.Header.Set("Cookie", "shopanda_csrf=token-bob")
	recB := httptest.NewRecorder()
	router.ServeHTTP(recB, reqB)

	if recA.Body.String() != recB.Body.String() {
		t.Fatal("two guests must receive byte-identical cached PDP HTML")
	}
	body := recA.Body.String()
	if strings.Contains(body, "token-alice") || strings.Contains(body, "token-bob") {
		t.Fatal("CSRF cookie values must not appear in cached HTML")
	}
	if strings.Contains(body, "cust-") || strings.Contains(body, "alice") || strings.Contains(body, "bob") {
		t.Fatal("customer-identifying data must not appear in cached HTML")
	}
	if recB.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("second guest = %q, want HIT", recB.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_AuthStateSplitsKeyWithoutLeakingIdentity(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	alice, err := identity.NewIdentity("cust-alice", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	alice = alice.WithDisplayName("Alice Example")
	bob, err := identity.NewIdentity("cust-bob", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	bob = bob.WithDisplayName("Bob Example")

	recGuest := httptest.NewRecorder()
	router.ServeHTTP(recGuest, httptest.NewRequest(http.MethodGet, "/products/widget", nil))

	reqA := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	reqA = reqA.WithContext(auth.WithIdentity(reqA.Context(), alice))
	recA := httptest.NewRecorder()
	router.ServeHTTP(recA, reqA)

	reqB := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	reqB = reqB.WithContext(auth.WithIdentity(reqB.Context(), bob))
	recB := httptest.NewRecorder()
	router.ServeHTTP(recB, reqB)

	if recGuest.Body.String() == recA.Body.String() {
		t.Fatal("authenticated shell must differ from guest (auth_state is in the key because signed-in chrome differs)")
	}
	if recA.Body.String() != recB.Body.String() {
		t.Fatal("two authenticated customers must share one cache entry")
	}
	if strings.Contains(recA.Body.String(), "Alice Example") || strings.Contains(recB.Body.String(), "Bob Example") {
		t.Fatal("display names must not be baked into cached HTML")
	}
	if strings.Contains(recA.Body.String(), "cust-alice") || strings.Contains(recA.Body.String(), "cust-bob") {
		t.Fatal("customer IDs must not appear in cached HTML")
	}
}

func TestFullPageCache_CSRFTokenNeverStored(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	id, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req.Header.Set("Cookie", "shopanda_csrf=super-secret-csrf-token")
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "super-secret-csrf-token") {
		t.Fatal("request CSRF token leaked into cached HTML")
	}
	if !strings.Contains(body, `action="/account/logout"`) {
		t.Fatal("cacheable chrome should keep logout form; CSRF loads via fragment")
	}
	if !strings.Contains(body, `/fragments/csrf`) {
		t.Fatal("cacheable logout must include /fragments/csrf hole")
	}
	if !strings.Contains(body, `name="csrf_token"`) || !strings.Contains(body, `value=""`) {
		t.Fatal("cacheable logout must use empty csrf_token skeleton / fragment placeholder")
	}
	if strings.Contains(body, `value="super-secret-csrf-token"`) {
		t.Fatal("filled csrf_token must not appear in cacheable HTML")
	}
	if rec.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("header = %q, want MISS (storeable with fragment hole)", rec.Header().Get("X-Shopanda-Cache"))
	}
	for _, raw := range backend.entries {
		if strings.Contains(string(raw), "super-secret-csrf-token") {
			t.Fatal("stored FPC payload contains request CSRF token")
		}
	}
}

func TestFullPageCache_LogoutWithoutCSRFFragmentBypasses(t *testing.T) {
	backend := newFPCMemCache()
	// Theme that keeps logout + empty CSRF without the fragment hole (custom-theme footgun).
	dir := t.TempDir()
	mustWrite := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("theme.yaml", "name: bare\nversion: \"0.1.0\"\n")
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite("templates/layout.html", `<!DOCTYPE html><html><body>{{ template "content" . }}</body></html>`)
	mustWrite("templates/product.html", `{{ define "title" }}p{{ end }}{{ define "content" }}`+
		`<form action="/account/logout" method="post"><input type="hidden" name="csrf_token" value="{{ .Layout.CSRFToken }}"></form>`+
		`<h1>{{ .Product.Name }}</h1>{{ end }}{{ template "layout.html" . }}`)
	engine, err := themeapp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fpcProductRepo{mockStorefrontRepo: mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug}, nil
		},
	}}
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	id, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("header = %q, want BYPASS for logout without /fragments/csrf", rec.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 0 {
		t.Fatal("must not store logout form without CSRF fragment hole")
	}
}

func TestFullPageCache_UnknownRouteNotCachedEvenIfItLooksLikePDP(t *testing.T) {
	if cacheapp.Allowlisted("/products/{slug}/reviews") {
		t.Fatal("reviews must stay off the allowlist")
	}
	if cacheapp.Cacheable("/products/{slug}", "/cart/add") {
		t.Fatal("denylist must win over allowlist")
	}
}

func TestFullPageCache_AuthenticatedKeepsNoStore(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := httpshared.CacheControlMiddleware(nil)(newStorefrontRouter(h))
	id, err := identity.NewIdentity("cust-a", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req1 = req1.WithContext(auth.WithIdentity(req1.Context(), id))
	router.ServeHTTP(rec1, req1)
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("miss header = %q", rec1.Header().Get("X-Shopanda-Cache"))
	}
	if cc := rec1.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("auth miss Cache-Control = %q, want no-store", cc)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req2 = req2.WithContext(auth.WithIdentity(req2.Context(), id))
	router.ServeHTTP(rec2, req2)
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("hit header = %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
	if cc := rec2.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("auth hit Cache-Control = %q, want no-store", cc)
	}
}

func TestFullPageCache_GuestPublicMaxAgeWithMiddleware(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := httpshared.CacheControlMiddleware(nil)(newStorefrontRouter(h))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("header = %q", rec.Header().Get("X-Shopanda-Cache"))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "public, max-age=") || cc == "public, max-age=300" {
		t.Fatalf("guest Cache-Control = %q, want FPC remaining max-age not middleware 300", cc)
	}
}

func TestFullPageCache_HomeAndSearchMissThenHit(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	home1 := httptest.NewRecorder()
	router.ServeHTTP(home1, httptest.NewRequest(http.MethodGet, "/", nil))
	if home1.Code != http.StatusOK || home1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("home miss status=%d header=%q", home1.Code, home1.Header().Get("X-Shopanda-Cache"))
	}
	home2 := httptest.NewRecorder()
	router.ServeHTTP(home2, httptest.NewRequest(http.MethodGet, "/", nil))
	if home2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("home hit = %q", home2.Header().Get("X-Shopanda-Cache"))
	}

	search1 := httptest.NewRecorder()
	router.ServeHTTP(search1, httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if search1.Code != http.StatusOK || search1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("search miss status=%d header=%q", search1.Code, search1.Header().Get("X-Shopanda-Cache"))
	}
	search2 := httptest.NewRecorder()
	router.ServeHTTP(search2, httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if search2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("search hit = %q", search2.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_CMSPageMissThenHit(t *testing.T) {
	backend := newFPCMemCache()
	page, err := cms.NewPage("pg1", "about", "About us", "<p>Hello</p>")
	if err != nil {
		t.Fatal(err)
	}
	h := fpcHandler(t, backend).WithContentBlocks(nil, nil, &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, slug string) (*cms.Page, error) {
			if slug == "about" {
				return page, nil
			}
			return nil, nil
		},
	})
	router := newStorefrontRouter(h)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/pages/about", nil))
	if rec1.Code != http.StatusOK || rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("cms miss status=%d header=%q body=%s", rec1.Code, rec1.Header().Get("X-Shopanda-Cache"), rec1.Body.String())
	}
	var pageTagged bool
	for _, tags := range backend.tags {
		for _, tag := range tags {
			if tag == "page:pg1" {
				pageTagged = true
			}
		}
	}
	if !pageTagged {
		t.Fatalf("missing CMS page tag: %#v", backend.tags)
	}
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/pages/about", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("cms hit = %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_Non200NotStored(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	notFound := httptest.NewRecorder()
	router.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/products/missing", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", notFound.Code)
	}
	if notFound.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("404 header = %q, want BYPASS", notFound.Header().Get("X-Shopanda-Cache"))
	}

	fail := httptest.NewRecorder()
	router.ServeHTTP(fail, httptest.NewRequest(http.MethodGet, "/products/boom", nil))
	if fail.Code != http.StatusInternalServerError {
		t.Fatalf("boom status = %d", fail.Code)
	}
	if fail.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("500 header = %q, want BYPASS", fail.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 0 {
		t.Fatalf("non-200 must not store, got %d entries", len(backend.entries))
	}
}

func TestFullPageCache_QueryNoiseDoesNotCreateSecondKey(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend)
	router := newStorefrontRouter(h)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/products/widget?utm_source=ad&fbclid=abc", nil))
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("first = %q", rec1.Header().Get("X-Shopanda-Cache"))
	}
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("clean URL should HIT the tracking-param miss, got %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(backend.entries))
	}

	search1 := httptest.NewRecorder()
	router.ServeHTTP(search1, httptest.NewRequest(http.MethodGet, "/search?q=oak&utm_source=ad", nil))
	search2 := httptest.NewRecorder()
	router.ServeHTTP(search2, httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if search2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("search tracking strip: %q", search2.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_RefusesIdentityInBody(t *testing.T) {
	backend := newFPCMemCache()
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: "hello alice example"}, nil
			},
		},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	alice, err := identity.NewIdentity("cust-alice", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	alice = alice.WithDisplayName("Alice Example")
	req := httptest.NewRequest(http.MethodGet, "/products/widget", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), alice))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("personalized HTML header = %q, want BYPASS", rec.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 0 {
		t.Fatal("must not store HTML that contains the shopper display name")
	}
}

type fpcNavAttrs struct {
	attrs []catalog.Attribute
}

func (f fpcNavAttrs) ListLayeredNavAttributes(context.Context) ([]catalog.Attribute, error) {
	return f.attrs, nil
}

func TestFullPageCache_JunkAttrDoesNotSplitListingKey(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend).WithLayeredNavAttributes(fpcNavAttrs{attrs: []catalog.Attribute{{
		Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, UseInLayeredNav: true,
	}}})
	router := newStorefrontRouter(h)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/search?q=oak&attr_notreal=zzzz", nil))
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("first = %q", rec1.Header().Get("X-Shopanda-Cache"))
	}
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("junk attr_* must not mint a second key, got %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(backend.entries))
	}

	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/search?q=oak&attr_color=red", nil))
	if rec3.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("configured attr_color should vary the key, got %q", rec3.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 2 {
		t.Fatalf("entries after real attr = %d, want 2", len(backend.entries))
	}
}

type fpcNavErr struct{}

func (fpcNavErr) ListLayeredNavAttributes(context.Context) ([]catalog.Attribute, error) {
	return nil, errors.New("attrs down")
}

func TestFullPageCache_AttrAllowlistErrorBypasses(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend).WithLayeredNavAttributes(fpcNavErr{})
	router := newStorefrontRouter(h)

	// Prime an unfiltered search entry while the allowlist is healthy.
	okHandler := fpcHandler(t, backend)
	okRouter := newStorefrontRouter(okHandler)
	okRouter.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if len(backend.entries) != 1 {
		t.Fatalf("setup entries = %d, want 1", len(backend.entries))
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/search?q=oak&attr_color=red", nil))
	if rec.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("allowlist failure header = %q, want BYPASS (must not HIT the unfiltered key)", rec.Header().Get("X-Shopanda-Cache"))
	}
	if rec.Header().Get("X-Shopanda-Cache") == "HIT" {
		t.Fatal("must not share the unfiltered search entry when attr allowlist cannot load")
	}
	if len(backend.entries) != 1 {
		t.Fatalf("must not store a scrubbed-empty key, entries = %d", len(backend.entries))
	}
}

type fpcAdvAttrs struct {
	attrs []catalog.Attribute
}

func (f fpcAdvAttrs) ListAdvancedSearchAttributes(context.Context) ([]catalog.Attribute, error) {
	return f.attrs, nil
}

func TestFullPageCache_AdvancedSearchAttrVariesSearchNotPLP(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend).WithAdvancedSearchAttributes(fpcAdvAttrs{attrs: []catalog.Attribute{{
		Code: "brand", Label: "Brand", Type: catalog.AttributeTypeSelect, UseInAdvancedSearch: true,
	}}})
	router := newStorefrontRouter(h)

	search1 := httptest.NewRecorder()
	router.ServeHTTP(search1, httptest.NewRequest(http.MethodGet, "/search?q=oak&attr_brand=acme", nil))
	if search1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("search attr miss = %q", search1.Header().Get("X-Shopanda-Cache"))
	}
	search2 := httptest.NewRecorder()
	router.ServeHTTP(search2, httptest.NewRequest(http.MethodGet, "/search?q=oak", nil))
	if search2.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("search without brand must be a distinct key, got %q", search2.Header().Get("X-Shopanda-Cache"))
	}

	plp1 := httptest.NewRecorder()
	router.ServeHTTP(plp1, httptest.NewRequest(http.MethodGet, "/products?attr_brand=acme", nil))
	if plp1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("plp miss = %q", plp1.Header().Get("X-Shopanda-Cache"))
	}
	plp2 := httptest.NewRecorder()
	router.ServeHTTP(plp2, httptest.NewRequest(http.MethodGet, "/products", nil))
	if plp2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("PLP must drop advanced-search-only attr_brand, got %q", plp2.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_OversizedBodyBypass(t *testing.T) {
	backend := newFPCMemCache()
	repo := &fpcProductRepo{mockStorefrontRepo: mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: strings.Repeat("w", cacheapp.MaxPageBytes)}, nil
		},
	}}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	rec := httptest.NewRecorder()
	newStorefrontRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("header = %q, want BYPASS", rec.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 0 {
		t.Fatalf("oversized HTML must not be stored, got %d", len(backend.entries))
	}
}

func TestFullPageCache_CSRFMentionInCopyStillCached(t *testing.T) {
	backend := newFPCMemCache()
	page, err := cms.NewPage("pg1", "about", "About us", "<p>Never bake a csrf_token into a shared page.</p>")
	if err != nil {
		t.Fatal(err)
	}
	h := fpcHandler(t, backend).WithContentBlocks(nil, nil, &mockPageRepo{
		findActiveBySlugFn: func(_ context.Context, slug string) (*cms.Page, error) {
			if slug == "about" {
				return page, nil
			}
			return nil, nil
		},
	})
	router := newStorefrontRouter(h)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/pages/about", nil))
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("copy mentioning csrf_token must still be cacheable, got %q body=%s", rec1.Header().Get("X-Shopanda-Cache"), rec1.Body.String())
	}
	if !strings.Contains(rec1.Body.String(), "csrf_token") {
		t.Fatal("expected merchandising copy to include csrf_token")
	}
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/pages/about", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("second = %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_CategoryTreeTagsOnCategoriesIndex(t *testing.T) {
	backend := newFPCMemCache()
	h := storefront.NewStorefrontHandler(createTestTheme(t), &fpcProductRepo{}, &mockStorefrontCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return []catalog.Category{
				{ID: "empty-cat", Name: "Empty", Slug: "empty"},
				{ID: "nav-cat", Name: "Nav", Slug: "nav"},
			}, nil
		},
	}, composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend, time.Minute, true, nil)
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/categories", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("status=%d header=%q", rec.Code, rec.Header().Get("X-Shopanda-Cache"))
	}
	want := map[string]bool{"category:empty-cat": false, "category:nav-cat": false}
	for _, tags := range backend.tags {
		for _, tag := range tags {
			if _, ok := want[tag]; ok {
				want[tag] = true
			}
		}
	}
	for tag, found := range want {
		if !found {
			t.Fatalf("missing %s in %#v", tag, backend.tags)
		}
	}
}

func TestFullPageCache_DegradedContentNotStored(t *testing.T) {
	backend := newFPCMemCache()
	blocks := &mockContentBlockRepo{
		findByTargetFn: func(_ context.Context, _ cms.TargetType, _ string) ([]*cms.ContentBlock, error) {
			return nil, errors.New("blocks down")
		},
	}
	h := fpcHandler(t, backend).WithContentBlocks(blocks, cmsApp.NewBlockResolver(nil), nil)
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("home should still render best-effort, status=%d", rec.Code)
	}
	if rec.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("header = %q, want BYPASS", rec.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend.entries) != 0 {
		t.Fatalf("degraded home must not be stored, got %d", len(backend.entries))
	}

	backend2 := newFPCMemCache()
	h2 := storefront.NewStorefrontHandler(createTestTheme(t), &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget"}, nil
			},
		},
	}, &mockStorefrontCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return nil, errors.New("cats down")
		},
	}, composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	).WithFullPageCache(backend2, time.Minute, true, nil)
	rec2 := httptest.NewRecorder()
	newStorefrontRouter(h2).ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("PDP should still render, status=%d", rec2.Code)
	}
	if rec2.Header().Get("X-Shopanda-Cache") != "BYPASS" {
		t.Fatalf("nav-load failure header = %q, want BYPASS", rec2.Header().Get("X-Shopanda-Cache"))
	}
	if len(backend2.entries) != 0 {
		t.Fatal("empty-nav PDP must not be stored")
	}
}

func TestFullPageCache_ListCategoryIDsSkippedWithoutTagBag(t *testing.T) {
	repo := &fpcProductRepo{
		mockStorefrontRepo: mockStorefrontRepo{
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget"}, nil
			},
		},
		catsByProduct: map[string][]string{"p1": {"c1"}},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	rec := httptest.NewRecorder()
	newStorefrontRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if repo.listCalls != 0 {
		t.Fatalf("ListCategoryIDsByProduct calls = %d, want 0 when FPC is off", repo.listCalls)
	}
}

func TestFullPageCache_RepeatedPageKeepsFirstValue(t *testing.T) {
	backend := newFPCMemCache()
	var seenOffset int
	search := &mockSearchEngine{searchFn: func(_ context.Context, q search.SearchQuery) (search.SearchResult, error) {
		seenOffset = q.Offset
		return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}, Total: 0}, nil
	}}
	h := storefront.NewStorefrontHandler(createTestTheme(t), &fpcProductRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		search,
	).WithFullPageCache(backend, time.Minute, true, nil)
	rec := httptest.NewRecorder()
	newStorefrontRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products?page=2&page=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if seenOffset != 12 {
		t.Fatalf("Offset = %d, want 12 (page 2 with per_page 12)", seenOffset)
	}
	if rec.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("header = %q", rec.Header().Get("X-Shopanda-Cache"))
	}
}

func TestFullPageCache_CSPNonceRotatedOnHit(t *testing.T) {
	backend := newFPCMemCache()
	h := fpcHandler(t, backend).WithCSPEnabled(true)
	router := newStorefrontRouter(h)

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec1.Header().Get("X-Shopanda-Cache") != "MISS" {
		t.Fatalf("miss header = %q", rec1.Header().Get("X-Shopanda-Cache"))
	}
	csp1 := rec1.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp1, "nonce-") {
		t.Fatalf("miss CSP missing nonce: %q", csp1)
	}
	nonce1Start := strings.Index(csp1, "nonce-") + len("nonce-")
	nonce1End := strings.Index(csp1[nonce1Start:], "'")
	nonce1 := csp1[nonce1Start : nonce1Start+nonce1End]
	nonce1HTML := strings.ReplaceAll(nonce1, "+", "&#43;")
	if nonce1 == "" || !strings.Contains(rec1.Body.String(), nonce1HTML) {
		t.Fatalf("miss HTML must embed nonce %q (escaped %q)", nonce1, nonce1HTML)
	}

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/products/widget", nil))
	if rec2.Header().Get("X-Shopanda-Cache") != "HIT" {
		t.Fatalf("hit header = %q", rec2.Header().Get("X-Shopanda-Cache"))
	}
	csp2 := rec2.Header().Get("Content-Security-Policy")
	nonce2Start := strings.Index(csp2, "nonce-") + len("nonce-")
	nonce2End := strings.Index(csp2[nonce2Start:], "'")
	nonce2 := csp2[nonce2Start : nonce2Start+nonce2End]
	nonce2HTML := strings.ReplaceAll(nonce2, "+", "&#43;")
	if nonce2 == "" || nonce2 == nonce1 {
		t.Fatalf("HIT must rotate nonce; miss=%q hit=%q", nonce1, nonce2)
	}
	if strings.Contains(rec2.Body.String(), nonce1HTML) || strings.Contains(rec2.Body.String(), nonce1) {
		t.Fatal("HIT HTML still contains the miss nonce")
	}
	if !strings.Contains(rec2.Body.String(), nonce2HTML) {
		t.Fatal("HIT HTML must contain the rotated nonce (HTML-escaped)")
	}
	if !strings.Contains(csp2, "nonce-"+nonce2) {
		t.Fatalf("HIT CSP must use rotated nonce: %q", csp2)
	}
}
