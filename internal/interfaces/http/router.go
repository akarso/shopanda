package http

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

// Handler returns the final http.Handler with all middleware applied.
func (r *Router) Handler() http.Handler {
	var h http.Handler = r.mux
	// Apply middleware in reverse order so the first Use() runs outermost.
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}
	return h
}
