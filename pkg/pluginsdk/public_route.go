package pluginsdk

import "github.com/akarso/shopanda/internal/platform/plugin"

// PublicRouteOption configures Host.RegisterPublicRoute.
type PublicRouteOption func(*publicRouteOpts)

type publicRouteOpts struct {
	noCache bool
}

// NoCache marks the public route for Cache-Control no-store (guest GET/HEAD
// would otherwise receive the default public max-age policy).
func NoCache() PublicRouteOption {
	return func(o *publicRouteOpts) { o.noCache = true }
}

func toCorePublicRouteOpts(opts []PublicRouteOption) []plugin.PublicRouteOption {
	cfg := &publicRouteOpts{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if !cfg.noCache {
		return nil
	}
	return []plugin.PublicRouteOption{plugin.WithNoCache()}
}
