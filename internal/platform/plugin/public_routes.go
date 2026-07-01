package plugin

import (
	"fmt"
	"net/http"
)

// PublicRoute registers a storefront or integrator HTTP handler mounted after InitAll.
type PublicRoute struct {
	Pattern string
	Handler http.Handler
}

// RegisterPublicRoute adds a public route mounted by the application after InitAll.
func (a *App) RegisterPublicRoute(pattern string, handler http.Handler) error {
	if pattern == "" {
		panic("plugin: public route pattern must not be empty")
	}
	if handler == nil {
		panic("plugin: public route handler must not be nil")
	}
	if err := validateRoutePattern(pattern); err != nil {
		return err
	}
	for _, route := range a.publicRoutes {
		if route.Pattern == pattern {
			return fmt.Errorf("plugin: duplicate public route pattern %q", pattern)
		}
	}
	a.publicRoutes = append(a.publicRoutes, PublicRoute{
		Pattern: pattern,
		Handler: handler,
	})
	return nil
}

// validateRoutePattern reports whether pattern is a syntactically valid
// net/http.ServeMux pattern. ServeMux panics on malformed patterns, so we
// probe it on a throwaway mux and convert the panic into a registration error.
func validateRoutePattern(pattern string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin: invalid public route pattern %q: %v", pattern, rec)
		}
	}()
	http.NewServeMux().Handle(pattern, http.NotFoundHandler())
	return nil
}

// PublicRoutes returns registered public routes.
func (a *App) PublicRoutes() []PublicRoute {
	if len(a.publicRoutes) == 0 {
		return nil
	}
	out := make([]PublicRoute, len(a.publicRoutes))
	copy(out, a.publicRoutes)
	return out
}
