package shared_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
)

// withTestTracerProvider installs an SDK provider backed by an in-memory
// exporter for the duration of the test, and restores the previous global
// provider on cleanup — TracingMiddleware always calls otel.Tracer(...) at
// package init, so the provider must be swapped in before the middleware
// runs, not before it's constructed.
func withTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exporter
}

func TestTracingMiddleware_RecordsRouteAndStatus(t *testing.T) {
	exporter := withTestTracerProvider(t)

	router := shared.NewRouter()
	router.HandleFunc("GET /api/v1/widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.Use(shared.TracingMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/widgets/42", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]

	if span.Name != "HTTP GET" {
		t.Errorf("span name = %q, want %q", span.Name, "HTTP GET")
	}
	attrs := attribute.NewSet(span.Attributes...)
	if route, ok := attrs.Value("http.route"); !ok || route.AsString() != "GET /api/v1/widgets/{id}" {
		t.Errorf("http.route = %v, want the matched template", route)
	}
	if code, ok := attrs.Value("http.response.status_code"); !ok || code.AsInt64() != http.StatusOK {
		t.Errorf("http.response.status_code = %v, want 200", code)
	}
	// The raw URL path (as opposed to the templated http.route above) must
	// never be recorded: it can carry a customer/order ID or a
	// reset/verification token, and spans export to a configurable —
	// possibly third-party — OTLP endpoint.
	if _, ok := attrs.Value("url.path"); ok {
		t.Error("span must not carry a raw url.path attribute")
	}
	if span.Status.Code != codes.Unset && span.Status.Code != codes.Ok {
		t.Errorf("status code = %v, want Unset/Ok for a 200 response", span.Status.Code)
	}
}

func TestTracingMiddleware_RecordsErrorStatusOn5xx(t *testing.T) {
	exporter := withTestTracerProvider(t)

	router := shared.NewRouter()
	router.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	router.Use(shared.TracingMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("status code = %v, want Error for a 5xx response", spans[0].Status.Code)
	}
}

func TestTracingMiddleware_UnmatchedRouteUsesBoundedLabel(t *testing.T) {
	exporter := withTestTracerProvider(t)

	router := shared.NewRouter()
	router.HandleFunc("GET /known", func(w http.ResponseWriter, r *http.Request) {})
	router.Use(shared.TracingMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/no/such/route", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := attribute.NewSet(spans[0].Attributes...)
	if route, ok := attrs.Value("http.route"); !ok || route.AsString() != "unmatched" {
		t.Errorf("http.route = %v, want %q for a 404", route, "unmatched")
	}
}
