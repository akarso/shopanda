package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// stubStoreRepo implements store.StoreRepository for middleware tests.
type stubStoreRepo struct {
	byDomain map[string]*store.Store
	byID     map[string]*store.Store
	byCode   map[string]*store.Store
	def      *store.Store
	all      []store.Store
	err      error
}

func (s *stubStoreRepo) FindByID(_ context.Context, id string) (*store.Store, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byID[id], nil
}

func (s *stubStoreRepo) FindByCode(_ context.Context, code string) (*store.Store, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byCode[code], nil
}

func (s *stubStoreRepo) FindByDomain(_ context.Context, domain string) (*store.Store, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byDomain[domain], nil
}

func (s *stubStoreRepo) FindDefault(_ context.Context) (*store.Store, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.def, nil
}

func (s *stubStoreRepo) FindAll(_ context.Context) ([]store.Store, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.all, nil
}

func (s *stubStoreRepo) Create(_ context.Context, st *store.Store) error { return s.err }
func (s *stubStoreRepo) Update(_ context.Context, st *store.Store) error { return s.err }

func storeTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func TestStoreMiddleware_ResolvesByDomain(t *testing.T) {
	now := time.Now()
	de := store.NewStoreFromDB("s-de", "de", "Germany", "EUR", "DE", "shop.de", false, now, now)

	repo := &stubStoreRepo{byDomain: map[string]*store.Store{"shop.de": de}}
	mw := shophttp.StoreMiddleware(repo, storeTestLogger())

	var got *store.Store
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = store.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://shop.de/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got == nil {
		t.Fatal("store not in context")
	}
	if got.ID != "s-de" {
		t.Errorf("store.ID = %q, want s-de", got.ID)
	}
}

func TestStoreMiddleware_FallsBackToDefault(t *testing.T) {
	now := time.Now()
	def := store.NewStoreFromDB("s-def", "default", "Default", "USD", "US", "", true, now, now)

	repo := &stubStoreRepo{
		byDomain: map[string]*store.Store{},
		def:      def,
	}
	mw := shophttp.StoreMiddleware(repo, storeTestLogger())

	var got *store.Store
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = store.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://unknown.com/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got == nil {
		t.Fatal("store not in context")
	}
	if got.ID != "s-def" {
		t.Errorf("store.ID = %q, want s-def", got.ID)
	}
}

func TestStoreMiddleware_NoStore(t *testing.T) {
	repo := &stubStoreRepo{byDomain: map[string]*store.Store{}}
	mw := shophttp.StoreMiddleware(repo, storeTestLogger())

	var got *store.Store
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = store.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://unknown.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got != nil {
		t.Errorf("expected nil store, got %+v", got)
	}
}

func TestStoreMiddleware_StripsPort(t *testing.T) {
	now := time.Now()
	s := store.NewStoreFromDB("s-1", "local", "Local", "EUR", "DE", "localhost", false, now, now)

	repo := &stubStoreRepo{byDomain: map[string]*store.Store{"localhost": s}}
	mw := shophttp.StoreMiddleware(repo, storeTestLogger())

	var got *store.Store
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = store.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://localhost:8080/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got == nil {
		t.Fatal("store not in context")
	}
	if got.ID != "s-1" {
		t.Errorf("store.ID = %q, want s-1", got.ID)
	}
}

func TestStoreMiddleware_DomainLookupError_FallsBackToDefault(t *testing.T) {
	now := time.Now()
	def := store.NewStoreFromDB("s-def", "default", "Default", "USD", "US", "", true, now, now)

	// Use a stub where FindByDomain errors but FindDefault succeeds.
	domainErrRepo := &domainErrorRepo{def: def}
	mw := shophttp.StoreMiddleware(domainErrRepo, storeTestLogger())

	var got *store.Store
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = store.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "http://broken.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got == nil {
		t.Fatal("expected fallback to default store")
	}
	if got.ID != "s-def" {
		t.Errorf("store.ID = %q, want s-def", got.ID)
	}
}

// domainErrorRepo always errors on FindByDomain but falls through to FindDefault.
type domainErrorRepo struct {
	def *store.Store
}

func (r *domainErrorRepo) FindByDomain(_ context.Context, _ string) (*store.Store, error) {
	return nil, errors.New("lookup failed")
}
func (r *domainErrorRepo) FindDefault(_ context.Context) (*store.Store, error) {
	return r.def, nil
}
func (r *domainErrorRepo) FindByID(_ context.Context, _ string) (*store.Store, error) {
	return nil, nil
}
func (r *domainErrorRepo) FindByCode(_ context.Context, _ string) (*store.Store, error) {
	return nil, nil
}
func (r *domainErrorRepo) FindAll(_ context.Context) ([]store.Store, error) { return nil, nil }
func (r *domainErrorRepo) Create(_ context.Context, _ *store.Store) error   { return nil }
func (r *domainErrorRepo) Update(_ context.Context, _ *store.Store) error   { return nil }
