package storefront_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func fragmentRouter(t *testing.T, h *storefront.StorefrontHandler) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fragments/csrf", h.CSRFFragment())
	mux.HandleFunc("GET /fragments/greeting", h.GreetingFragment())
	mux.HandleFunc("GET /fragments/cart-count", h.CartCountFragment())
	mux.HandleFunc("GET /fragments/mini-cart", h.MiniCartFragment())
	mux.HandleFunc("GET /fragments/recently-viewed", h.RecentlyViewedFragment())
	return mux
}

func TestFragment_CSRFReturnsNoStorePerRequestToken(t *testing.T) {
	h := storefront.NewStorefrontHandler(createTestTheme(t), &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	router := fragmentRouter(t, h)

	req1 := httptest.NewRequest(http.MethodGet, "/fragments/csrf", nil)
	req1.AddCookie(&http.Cookie{Name: "shopanda_csrf", Value: "token-one"})
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rec1.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec1.Body.String(), `value="token-one"`) {
		t.Fatalf("body = %q, want token-one", rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/fragments/csrf", nil)
	req2.AddCookie(&http.Cookie{Name: "shopanda_csrf", Value: "token-two"})
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), `value="token-two"`) {
		t.Fatalf("body = %q, want token-two", rec2.Body.String())
	}
	if rec1.Body.String() == rec2.Body.String() {
		t.Fatal("different sessions must receive different CSRF fragment HTML")
	}
}

func TestFragment_CSRFMintsCookieWhenMissing(t *testing.T) {
	h := storefront.NewStorefrontHandler(createTestTheme(t), &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	rec := httptest.NewRecorder()
	fragmentRouter(t, h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fragments/csrf", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var token string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_csrf" && c.Value != "" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("CSRF fragment must Set-Cookie shopanda_csrf when missing")
	}
	if !strings.Contains(rec.Body.String(), `value="`+token+`"`) {
		t.Fatalf("body must embed minted token; body=%q", rec.Body.String())
	}
}

func TestFragment_GreetingReflectsIdentity(t *testing.T) {
	h := storefront.NewStorefrontHandler(createTestTheme(t), &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	router := fragmentRouter(t, h)

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, "/fragments/greeting", nil))
	if guest.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", guest.Header().Get("Cache-Control"))
	}
	if !strings.Contains(guest.Body.String(), "Account") {
		t.Fatalf("guest greeting = %q", guest.Body.String())
	}

	alice, err := identity.NewIdentity("cust-a", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	alice = alice.WithDisplayName("Alice Example")
	req := httptest.NewRequest(http.MethodGet, "/fragments/greeting", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), alice))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Alice Example") {
		t.Fatalf("authenticated greeting = %q", rec.Body.String())
	}
}

func TestFragment_RecentlyViewedUsesCookieHistory(t *testing.T) {
	repo := &mockStorefrontRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == "p1" {
				return &catalog.Product{ID: "p1", Name: "Oak Desk", Slug: "oak-desk"}, nil
			}
			return nil, nil
		},
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Oak Desk", Slug: slug}, nil
		},
	}
	h := storefront.NewStorefrontHandler(createTestTheme(t), repo, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	router := fragmentRouter(t, h)

	miss := httptest.NewRecorder()
	router.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/fragments/recently-viewed", nil))
	if miss.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", miss.Header().Get("Cache-Control"))
	}
	if !strings.Contains(miss.Body.String(), "No recently viewed") {
		t.Fatalf("empty history body = %q", miss.Body.String())
	}

	hit := httptest.NewRecorder()
	router.ServeHTTP(hit, httptest.NewRequest(http.MethodGet, "/fragments/recently-viewed?add=p1", nil))
	if !strings.Contains(hit.Body.String(), "Oak Desk") || !strings.Contains(hit.Body.String(), "/products/oak-desk") {
		t.Fatalf("recently viewed body = %q", hit.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range hit.Result().Cookies() {
		if c.Name == "shopanda_recently_viewed" {
			cookie = c
		}
	}
	if cookie == nil || !strings.Contains(cookie.Value, "p1") {
		t.Fatalf("fragment should set recently viewed cookie, got %#v", hit.Result().Cookies())
	}

	junk := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fragments/recently-viewed?add=not-a-product", nil)
	req.AddCookie(cookie)
	router.ServeHTTP(junk, req)
	for _, c := range junk.Result().Cookies() {
		if c.Name == "shopanda_recently_viewed" && strings.Contains(c.Value, "not-a-product") {
			t.Fatal("junk ?add= must not rewrite the recently-viewed cookie")
		}
	}
	if !strings.Contains(junk.Body.String(), "Oak Desk") {
		t.Fatalf("existing history should still render after junk add: %q", junk.Body.String())
	}
}

func TestFragment_CartAndMiniCartNoStore(t *testing.T) {
	h := storefront.NewStorefrontHandler(createTestTheme(t), &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](),
		composition.NewPipeline[composition.ListingContext](),
		newStorefrontSearchMock(),
	)
	router := fragmentRouter(t, h)
	for _, path := range []string{"/fragments/cart-count", "/fragments/mini-cart"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s Cache-Control = %q, want no-store", path, rec.Header().Get("Cache-Control"))
		}
	}
}
