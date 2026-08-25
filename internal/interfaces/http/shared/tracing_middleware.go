package shared

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware starts one span per HTTP request. Register it alongside
// MetricsMiddleware (order between the two does not matter) so the span
// covers the full request, including every other middleware.
//
// Like MetricsMiddleware, the route template isn't known until net/http's
// ServeMux has matched the request (see router.go's captureRoute/
// routeMatch), so it's recorded as a span attribute after next.ServeHTTP
// returns rather than embedded in the span name — the name stays the
// low-cardinality "HTTP {method}", per OTel semantic-convention guidance
// for when a route template isn't available at span-start time.
//
// The tracer is resolved via otel.Tracer(...) here, at call time, rather
// than cached in a package variable: OTel's global provider only migrates
// pre-existing Tracer handles to a newly-installed SDK provider once
// (sync.Once, internal/global/state.go) — a handle obtained before
// tracing.Setup runs would otherwise stay bound to whatever provider was
// global at that first call forever, even across later SetTracerProvider
// calls. Calling otel.Tracer at middleware-construction time (once per
// process, when wire_routes.go builds the router — well after Setup has
// run) sidesteps that entirely: it always resolves the current provider
// directly, no delegation involved. Before Setup runs (or when tracing is
// disabled), this is OTel's documented no-op tracer — safe and effectively
// free — so this middleware can be registered unconditionally.
func TracingMiddleware() Middleware {
	tracer := otel.Tracer("github.com/akarso/shopanda/internal/interfaces/http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// http.request.method is set as a structured attribute even
			// though the span name already embeds it — the name is for
			// human display/grouping, the attribute is what lets a trace
			// backend filter/aggregate by method. This is standard OTel
			// semantic-convention practice, not accidental duplication.
			//
			// Deliberately no raw URL/path attribute here: unlike
			// http.route (the templated pattern, e.g. "/products/{id}"),
			// the raw path can carry a customer ID, order ID, or a
			// password-reset/email-verification token in storefront and
			// admin routes — exactly what the metrics package's own
			// bounded-label policy exists to keep out of anything that
			// leaves the process, and tracing exports to a configurable
			// (possibly third-party) OTLP endpoint just like metrics would
			// if it allowed raw paths.
			ctx, span := tracer.Start(r.Context(), "HTTP "+r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
				),
			)
			defer span.End()

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))

			pattern := routeMatchFromContext(r.Context())
			if pattern == "" {
				pattern = unmatchedRoute
			}
			span.SetAttributes(
				attribute.String("http.route", pattern),
				attribute.Int("http.response.status_code", sw.status),
			)
			if sw.status >= 500 {
				span.SetStatus(codes.Error, "")
			}
		})
	}
}
