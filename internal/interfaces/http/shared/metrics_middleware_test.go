package shared_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

type recordedCall struct {
	route       string
	method      string
	statusClass string
	duration    time.Duration
}

type fakeRecorder struct {
	calls []recordedCall
}

func (f *fakeRecorder) HTTPRequest(route, method, statusClass string, d time.Duration) {
	f.calls = append(f.calls, recordedCall{route: route, method: method, statusClass: statusClass, duration: d})
}
func (f *fakeRecorder) CheckoutResult(string)  {}
func (f *fakeRecorder) JobFailure(string)      {}
func (f *fakeRecorder) WebhookDelivery(string) {}

var _ metrics.Recorder = (*fakeRecorder)(nil)

func TestMetricsMiddleware_UsesRouteTemplateAndStatusClass(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := &fakeRecorder{}
	router.Use(shared.MetricsMiddleware(rec))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/abc-123", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	call := rec.calls[0]
	if call.route != "GET /api/v1/products/{id}" {
		t.Errorf("route = %q, want the route template, not the raw path (got raw id in label would break cardinality)", call.route)
	}
	if call.method != http.MethodGet {
		t.Errorf("method = %q, want GET", call.method)
	}
	if call.statusClass != "2xx" {
		t.Errorf("statusClass = %q, want 2xx", call.statusClass)
	}
}

func TestMetricsMiddleware_UnrecognizedMethodUsesBoundedLabel(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := &fakeRecorder{}
	router.Use(shared.MetricsMiddleware(rec))

	// The HTTP method is an arbitrary token (RFC 7230); an unrecognized one
	// must not flow into the label unbounded.
	req := httptest.NewRequest("TRACK-ME-A1B2C3", "/api/v1/products/abc-123", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	if call := rec.calls[0]; call.method != "other" {
		t.Errorf("method = %q, want bounded label %q for an unrecognized method", call.method, "other")
	}
}

func TestMetricsMiddleware_UnmatchedRouteUsesBoundedLabel(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := &fakeRecorder{}
	router.Use(shared.MetricsMiddleware(rec))

	req := httptest.NewRequest(http.MethodGet, "/no/such/route/exists", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	if got := rec.calls[0].route; got != "unmatched" {
		t.Errorf("route = %q, want the fixed \"unmatched\" label (never the raw path) for a 404", got)
	}
	if got := rec.calls[0].statusClass; got != "4xx" {
		t.Errorf("statusClass = %q, want 4xx for a 404", got)
	}
}

func TestMetricsMiddleware_RecordsServerErrorStatusClass(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	rec := &fakeRecorder{}
	router.Use(shared.MetricsMiddleware(rec))

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if len(rec.calls) != 1 || rec.calls[0].statusClass != "5xx" {
		t.Fatalf("expected one 5xx call, got %+v", rec.calls)
	}
}

func TestMetricsMiddleware_NilRecorderDoesNotPanic(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.Use(shared.MetricsMiddleware(nil))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRouter_RoutePattern_ReturnsRegisteredTemplate(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/categories/{id}/products", func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/categories/42/products", nil)
	if got := router.RoutePattern(req); got != "GET /api/v1/categories/{id}/products" {
		t.Errorf("RoutePattern = %q, want the registered template", got)
	}
}

func TestRouter_RoutePattern_EmptyForNoMatch(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /known", func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/unknown/path/here", nil)
	if got := router.RoutePattern(req); got != "" {
		t.Errorf("RoutePattern = %q, want empty for unmatched request", got)
	}
}

// TestMetricsMiddleware_SurvivesRequestSwapBetweenMetricsAndDispatch pins the
// correctness of capturing the route pattern via a shared mutable box
// (internal/interfaces/http/shared/router.go's routeMatch) rather than
// re-deriving it independently in MetricsMiddleware. Real middleware between
// Metrics and the mux (auth, store resolution, language, CSRF, ...) all call
// r.WithContext(...), which allocates a NEW *http.Request — the one net/http
// ServeMux eventually populates .Pattern on is not the same pointer
// MetricsMiddleware holds. This test simulates that swap explicitly to
// confirm the pattern still reaches MetricsMiddleware afterward.
func TestMetricsMiddleware_SurvivesRequestSwapBetweenMetricsAndDispatch(t *testing.T) {
	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := &fakeRecorder{}
	router.Use(shared.MetricsMiddleware(rec))
	// Simulates any real middleware (auth, store resolution, ...) that
	// injects a context value via r.WithContext, replacing the *http.Request
	// pointer before the mux ever sees it.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type unrelatedKey struct{}
			swapped := r.WithContext(context.WithValue(r.Context(), unrelatedKey{}, "unrelated-value"))
			next.ServeHTTP(w, swapped)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/widgets/42", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	if got := rec.calls[0].route; got != "GET /api/v1/widgets/{id}" {
		t.Errorf("route = %q, want the matched template to survive the request swap", got)
	}
}
