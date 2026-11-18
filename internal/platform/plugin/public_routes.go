package plugin

import (
	"fmt"
	"net/http"
	"strings"
)

// PublicRoute registers a storefront or integrator HTTP handler mounted after InitAll.
type PublicRoute struct {
	Pattern string
	Handler http.Handler
	// NoCache marks the route path for Cache-Control no-store (merged into the
	// HTTP cache denylist at mount time).
	NoCache bool
}

// PublicRouteOption configures RegisterPublicRoute.
type PublicRouteOption func(*PublicRoute)

// WithNoCache marks the public route so guest GET/HEAD responses get no-store
// instead of the default public max-age cache policy.
func WithNoCache() PublicRouteOption {
	return func(r *PublicRoute) { r.NoCache = true }
}

// RegisterPublicRoute adds a public route mounted by the application after InitAll.
func (a *App) RegisterPublicRoute(pattern string, handler http.Handler, opts ...PublicRouteOption) error {
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
	route := PublicRoute{
		Pattern: pattern,
		Handler: handler,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&route)
		}
	}
	if route.NoCache {
		if err := validateNoCachePattern(route.Pattern); err != nil {
			return err
		}
	}
	a.publicRoutes = append(a.publicRoutes, route)
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

// NoCachePathPrefixes returns URL path prefixes for routes registered with
// WithNoCache, suitable for CacheControlMiddleware. The denylist entry is the
// longest static path before the first `{` (trailing `/` trimmed): e.g.
// "GET /api/x/{id}" → "/api/x". Middleware matches path == prefix ||
// HasPrefix(path, prefix+"/") — so siblings under that prefix also get
// no-store. RegisterPublicRoute rejects NoCache when a `{` cut leaves an empty
// or single-segment prefix (e.g. "/{id}", "/api/{v}/x" → "/api"); fully
// static one-segment mounts (e.g. "GET /customizer") are allowed.
func NoCachePathPrefixes(routes []PublicRoute) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, route := range routes {
		if !route.NoCache {
			continue
		}
		prefix := pathPrefixFromPattern(route.Pattern)
		if prefix == "" {
			continue
		}
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	return out
}

func pathPrefixFromPattern(pattern string) string {
	p, _ := staticPathBeforeParam(pattern)
	return strings.TrimRight(p, "/")
}

// validateNoCachePattern rejects NoCache when a `{` cut shortens the path to an
// empty or single-segment denylist prefix (would no-store a whole tree like
// /api). Fully static patterns — including one-segment mounts like
// "GET /customizer" — are allowed as-is.
func validateNoCachePattern(pattern string) error {
	path, cut := staticPathBeforeParam(pattern)
	prefix := strings.TrimRight(path, "/")
	if cut {
		if prefix == "" {
			return fmt.Errorf("plugin: NoCache pattern %q has no static path before '{' (leading path params cannot be denylisted)", pattern)
		}
		segs := strings.Split(strings.Trim(prefix, "/"), "/")
		if len(segs) < 2 || segs[0] == "" {
			return fmt.Errorf("plugin: NoCache pattern %q cuts at '{' to denylist prefix %q which is too broad (need at least /seg/seg before '{'; e.g. /api/{v}/… would denylist all of /api)", pattern, prefix)
		}
		return nil
	}
	if prefix == "" {
		return fmt.Errorf("plugin: NoCache pattern %q has no denylistable path", pattern)
	}
	return nil
}

// staticPathBeforeParam returns the URL path from pattern (method stripped) and
// whether a `{` cut shortened it. Trailing slash is preserved for the caller.
func staticPathBeforeParam(pattern string) (path string, cut bool) {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return "", false
	}
	if i := strings.IndexByte(p, ' '); i >= 0 {
		p = strings.TrimSpace(p[i+1:])
	}
	if p == "" {
		return "", false
	}
	if !strings.HasPrefix(p, "/") {
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[i:]
		} else {
			return "", false
		}
	}
	if i := strings.IndexByte(p, '{'); i >= 0 {
		return p[:i], true
	}
	return p, false
}
