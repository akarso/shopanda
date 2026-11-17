package sitekit

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin is the sitekit product external plugin (scaffolded by product-init).
type Plugin struct{}

// New returns the sitekit plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

// Name is the stable plugin identity and Migrator namespace.
func (p *Plugin) Name() string { return "sitekit" }

// Init registers HTTP routes and optional sync jobs. Enablement is compile-time
// registration in cmd/api/register_products.go (product-owned).
func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("sitekit plugin: host not configured")
	}
	if err := h.RegisterPublicRoute("GET /api/sitekit/health", http.HandlerFunc(health)); err != nil {
		return fmt.Errorf("sitekit plugin: health route: %w", err)
	}
	return nil
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","plugin":"sitekit"}`))
}
