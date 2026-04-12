package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
	apphttp "github.com/akarso/shopanda/internal/interfaces/http"
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

	mw := apphttp.ResolverMiddleware(repo, &fakeLogger{})
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

	mw := apphttp.ResolverMiddleware(repo, &fakeLogger{})
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

	mw := apphttp.ResolverMiddleware(repo, &fakeLogger{})
	handler := mw(next)

	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// --- RewriteHandler tests ---

func TestRewriteHandler_Resolve(t *testing.T) {
	rw := routing.NewURLRewriteFromDB("/test-slug", "category", "cat-456")
	ctx := routing.WithRewrite(context.Background(), rw)

	handler := apphttp.NewRewriteHandler()
	req := httptest.NewRequest(http.MethodGet, "/test-slug", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.Resolve().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Data struct {
			Type     string `json:"type"`
			EntityID string `json:"entity_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Type != "category" {
		t.Errorf("type = %q, want %q", resp.Data.Type, "category")
	}
	if resp.Data.EntityID != "cat-456" {
		t.Errorf("entity_id = %q, want %q", resp.Data.EntityID, "cat-456")
	}
}

func TestRewriteHandler_NoRewrite(t *testing.T) {
	handler := apphttp.NewRewriteHandler()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.Resolve().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
