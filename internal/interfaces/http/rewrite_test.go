package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
	apphttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- RewriteHandler tests ---
//
// ResolverMiddleware tests live in
// internal/interfaces/http/shared/resolver_middleware_test.go since the
// middleware itself moved to the shared package in PR-1021. RewriteHandler
// stays here — it's a request handler, not a cross-cutting primitive.

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
