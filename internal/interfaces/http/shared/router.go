package shared

import (
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
	r.mux.Handle(pattern, handler)
}

// HandleFunc registers a handler function for the given pattern.
func (r *Router) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.mux.HandleFunc(pattern, handler)
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
	r.mux.Handle(pattern, handler)
	return nil
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

// Handler returns the final http.Handler with all middleware applied.
func (r *Router) Handler() http.Handler {
	var h http.Handler = r.mux
	// Apply middleware in reverse order so the first Use() runs outermost.
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}
	return h
}
