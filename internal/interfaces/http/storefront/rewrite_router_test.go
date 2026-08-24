package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

// These tests exercise ResolverMiddleware and RewriteHandler wired together
// through a real shared.Router (net/http.ServeMux dispatch), mirroring how
// cmd/api/wire_routes.go registers them: a root "GET /{path...}" catch-all,
// reached only when no more specific route matches. Unlike
// resolver_middleware_test.go and rewrite_test.go, which call the middleware
// and handler directly, this confirms the actual route registration and
// ServeMux specificity resolution — the wiring gap this fixes.

type routerFakeRewriteRepo struct {
	rewrites map[string]*routing.URLRewrite
}

func (f *routerFakeRewriteRepo) FindByPath(_ context.Context, path string) (*routing.URLRewrite, error) {
	return f.rewrites[path], nil
}
func (f *routerFakeRewriteRepo) Save(_ context.Context, _ *routing.URLRewrite) error { return nil }
func (f *routerFakeRewriteRepo) Delete(_ context.Context, _ string) error            { return nil }

type routerFakeLogger struct{}

func (routerFakeLogger) Info(_ string, _ map[string]interface{})           {}
func (routerFakeLogger) Warn(_ string, _ map[string]interface{})           {}
func (routerFakeLogger) Error(_ string, _ error, _ map[string]interface{}) {}

func newResolverTestRouter(repo *routerFakeRewriteRepo) http.Handler {
	router := httpshared.NewRouter()
	// Mirrors a more specific SSR route registered ahead of the catch-all in
	// wire_routes.go, to prove it still wins on an exact path match.
	router.HandleFunc("GET /products/{slug}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // distinct sentinel, unrelated to rewrite JSON
	})
	router.Handle("GET /{path...}", httpshared.ResolverMiddleware(repo, routerFakeLogger{})(storefront.NewRewriteHandler().Resolve()))
	return router.Handler()
}

func TestRewriteRouting_ResolvesRenamedProductURL(t *testing.T) {
	rw := routing.NewURLRewriteFromDB("/old-nike-air-max", "product", "prod-123")
	handler := newResolverTestRouter(&routerFakeRewriteRepo{
		rewrites: map[string]*routing.URLRewrite{"/old-nike-air-max": rw},
	})

	req := httptest.NewRequest(http.MethodGet, "/old-nike-air-max", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
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
	if resp.Data.Type != "product" || resp.Data.EntityID != "prod-123" {
		t.Fatalf("unexpected resolution: %+v", resp.Data)
	}
}

func TestRewriteRouting_MoreSpecificRouteTakesPrecedence(t *testing.T) {
	// A path that would ALSO satisfy the catch-all must still be dispatched
	// to the more specific literal route, not the rewrite resolver.
	handler := newResolverTestRouter(&routerFakeRewriteRepo{rewrites: map[string]*routing.URLRewrite{}})

	req := httptest.NewRequest(http.MethodGet, "/products/current-slug", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (specific route should win over catch-all)", rec.Code, http.StatusTeapot)
	}
}

func TestRewriteRouting_UnknownPathNotFound(t *testing.T) {
	handler := newResolverTestRouter(&routerFakeRewriteRepo{rewrites: map[string]*routing.URLRewrite{}})

	req := httptest.NewRequest(http.MethodGet, "/never-existed", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
