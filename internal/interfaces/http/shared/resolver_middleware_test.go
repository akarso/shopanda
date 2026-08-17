package shared_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/interfaces/http/shared"
)

// --- fakes ---

type fakeRewriteRepo struct {
	rewrites map[string]*routing.URLRewrite
	err      error
}

func (f *fakeRewriteRepo) FindByPath(_ context.Context, path string) (*routing.URLRewrite, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rewrites[path], nil
}

func (f *fakeRewriteRepo) Save(_ context.Context, _ *routing.URLRewrite) error { return nil }
func (f *fakeRewriteRepo) Delete(_ context.Context, _ string) error            { return nil }

type fakeLogger struct{}

func (f *fakeLogger) Info(_ string, _ map[string]interface{})           {}
func (f *fakeLogger) Warn(_ string, _ map[string]interface{})           {}
func (f *fakeLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// --- ResolverMiddleware tests ---

func TestResolverMiddleware_Match(t *testing.T) {
	rw := routing.NewURLRewriteFromDB("/nike-air-max", "product", "abc-123")
	repo := &fakeRewriteRepo{rewrites: map[string]*routing.URLRewrite{"/nike-air-max": rw}}

	var captured *routing.URLRewrite
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = routing.RewriteFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := shared.ResolverMiddleware(repo, &fakeLogger{})
	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/nike-air-max", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured == nil {
		t.Fatal("expected rewrite in context, got nil")
	}
	if captured.EntityID() != "abc-123" {
		t.Errorf("entity_id = %q, want %q", captured.EntityID(), "abc-123")
	}
}

func TestResolverMiddleware_NoMatch(t *testing.T) {
	repo := &fakeRewriteRepo{rewrites: map[string]*routing.URLRewrite{}}

	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := shared.ResolverMiddleware(repo, &fakeLogger{})
	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/unknown-path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called on no match")
	}
}

func TestResolverMiddleware_RepoError(t *testing.T) {
	repo := &fakeRewriteRepo{err: errors.New("db down")}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called on repo error")
	})

	mw := shared.ResolverMiddleware(repo, &fakeLogger{})
	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
