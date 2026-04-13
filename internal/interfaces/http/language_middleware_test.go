package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func langFromHandler(mw shophttp.Middleware) func(r *http.Request) string {
	var lang string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang = translation.LanguageFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	return func(r *http.Request) string {
		lang = ""
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return lang
	}
}

func TestLanguageMiddleware_QueryParam(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products?lang=de", nil)
	got := resolve(req)
	if got != "de" {
		t.Errorf("language = %q, want de", got)
	}
}

func TestLanguageMiddleware_AcceptLanguageHeader(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products", nil)
	req.Header.Set("Accept-Language", "fr;q=0.9, en;q=0.8")
	got := resolve(req)
	if got != "fr" {
		t.Errorf("language = %q, want fr", got)
	}
}

func TestLanguageMiddleware_AcceptLanguageBCP47(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products", nil)
	req.Header.Set("Accept-Language", "pt-BR")
	got := resolve(req)
	if got != "pt-BR" {
		t.Errorf("language = %q, want pt-BR", got)
	}
}

func TestLanguageMiddleware_StoreDefault(t *testing.T) {
	now := time.Now()
	s := store.NewStoreFromDB("s-1", "de", "Germany", "EUR", "DE", "de", "", false, now, now)

	mw := shophttp.LanguageMiddleware()
	var lang string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang = translation.LanguageFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/products", nil)
	// Simulate store middleware having set the store in context.
	ctx := store.WithStore(req.Context(), s)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if lang != "de" {
		t.Errorf("language = %q, want de (from store)", lang)
	}
}

func TestLanguageMiddleware_DefaultEN(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products", nil)
	got := resolve(req)
	if got != "en" {
		t.Errorf("language = %q, want en (default)", got)
	}
}

func TestLanguageMiddleware_QueryParamOverridesHeader(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products?lang=es", nil)
	req.Header.Set("Accept-Language", "de")
	got := resolve(req)
	if got != "es" {
		t.Errorf("language = %q, want es (query param wins)", got)
	}
}

func TestLanguageMiddleware_InvalidQueryParamFallsThrough(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products?lang=abcde", nil)
	req.Header.Set("Accept-Language", "de")
	got := resolve(req)
	if got != "de" {
		t.Errorf("language = %q, want de (invalid query param should fall through)", got)
	}
}

func TestLanguageMiddleware_AcceptLanguageQOrdering(t *testing.T) {
	resolve := langFromHandler(shophttp.LanguageMiddleware())
	req := httptest.NewRequest("GET", "/products", nil)
	req.Header.Set("Accept-Language", "en;q=0.1, fr;q=1.0")
	got := resolve(req)
	if got != "fr" {
		t.Errorf("language = %q, want fr (highest q-value wins)", got)
	}
}
