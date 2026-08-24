package shared

import (
	"context"
	"fmt"
	"net/http"
)

// Router wraps http.ServeMux with middleware support.
type Router struct {
	mux        *http.ServeMux
	middleware []Middleware
}

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// NewRouter creates a new Router.
func NewRouter() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// Use appends middleware to the chain. Middleware executes in the order added.
func (r *Router) Use(mw ...Middleware) {
	r.middleware = append(r.middleware, mw...)
}

// Handle registers a handler for the given pattern.
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, r.captureRoute(handler))
}

// HandleFunc registers a handler function for the given pattern.
func (r *Router) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.mux.Handle(pattern, r.captureRoute(handler))
}

// TryHandle registers a handler, returning an error instead of panicking when
// the pattern is malformed or conflicts with an already-registered route.
// Use this for externally-supplied routes (e.g. plugin public routes) so a
// conflict surfaces as a startup error rather than aborting the process.
func (r *Router) TryHandle(pattern string, handler http.Handler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("router: cannot register %q: %v", pattern, rec)
		}
	}()
	r.mux.Handle(pattern, r.captureRoute(handler))
	return nil
}

// captureRoute wraps handler so that, once net/http.ServeMux has matched a
// request and populated Request.Pattern on it, the matched pattern is
// copied into the *routeMatch box threaded through this request's
// middleware chain (see Handler) — letting MetricsMiddleware read the
// matched route afterward without running a second, redundant routing pass
// (calling ServeMux.Handler(req) again just to learn what pattern the mux
// already dispatched through).
func (r *Router) captureRoute(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if m, ok := req.Context().Value(routeMatchKey{}).(*routeMatch); ok {
			m.pattern = req.Pattern
		}
		handler.ServeHTTP(w, req)
	})
}

// RoutePattern returns the registered ServeMux pattern that would handle req
// (e.g. "GET /api/v1/products/{id}"), without invoking any handler. Returns
// "" when nothing matches (e.g. a 404 or a mux-internal redirect).
//
// This exists so cross-cutting middleware (metrics) can label requests with
// a bounded route template instead of the raw URL path, which has unbounded
// cardinality (IDs, slugs, arbitrary 404 paths).
func (r *Router) RoutePattern(req *http.Request) string {
	_, pattern := r.mux.Handler(req)
	return pattern
}

// routeMatchKey is the context key for the *routeMatch box threaded through
// a single request's middleware chain (see Handler).
type routeMatchKey struct{}

// routeMatch is a mutable box carrying the route pattern resolved by the
// mux's own dispatch. It must be a pointer stashed in the context once, at
// the very outside of the middleware chain: net/http.ServeMux populates
// Request.Pattern (and PathValue) on a shallow copy of the request it
// passes to the matched handler, a copy that outer middleware (which called
// next.ServeHTTP with the pre-dispatch request) never observes directly.
// Sharing one mutable box by pointer, rather than by context value, lets a
// write made deep in the chain (in captureRoute, from inside the matched
// handler where Request.Pattern is already populated) be visible to a read
// made by outer middleware (MetricsMiddleware) after next.ServeHTTP returns
// — context.Value lookups walk up the parent chain regardless of how many
// intermediate WithContext calls happened, but the box's address doesn't
// change, so the mutation is visible through any of those copies.
type routeMatch struct {
	pattern string
}

// routeMatchFromContext returns the pattern captured for this request by
// Handler's dispatch, or "" if the request never reached the mux (e.g. a
// middleware short-circuited before it) or nothing matched.
func routeMatchFromContext(ctx context.Context) string {
	m, _ := ctx.Value(routeMatchKey{}).(*routeMatch)
	if m == nil {
		return ""
	}
	return m.pattern
}

// Handler returns the final http.Handler with all middleware applied.
func (r *Router) Handler() http.Handler {
	var h http.Handler = r.mux
	// Apply middleware in reverse order so the first Use() runs outermost.
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(context.WithValue(req.Context(), routeMatchKey{}, &routeMatch{}))
		h.ServeHTTP(w, req)
	})
}
