package main

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// registerProductPlugins is the product-owned registration hook.
// Product forks register their plugins here (and only here) so
// register_plugins.go stays an upstream-only compile-time list of
// in-tree demos and reference plugins.
//
// Seeded empty by scripts/product-init.sh when missing. Do not move
// extref / *demo registration into this file upstream.
func registerProductPlugins(registry *plugin.Registry, cfg *config.Config) {
	_ = registry
	_ = cfg
}
