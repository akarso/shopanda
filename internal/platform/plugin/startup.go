package plugin

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// LogStartup logs each plugin lifecycle state and the active infrastructure drivers.
func LogStartup(log logger.Logger, registry *Registry, cfg *config.Config) {
	if log == nil || registry == nil || cfg == nil {
		return
	}
	for _, e := range registry.Entries() {
		fields := map[string]interface{}{
			"plugin": e.Name,
			"state":  e.State,
		}
		if e.State == StateFailed && e.Err != nil {
			fields["error"] = e.Err.Error()
		}
		log.Info("plugin.status", fields)
	}
	log.Info("drivers.selected", map[string]interface{}{
		"search.engine":  cfg.Search.Engine,
		"cache.driver":   cfg.Cache.Driver,
		"queue.driver":   cfg.Queue.Driver,
		"media.storage":  cfg.Media.Storage,
		"payment.stripe": cfg.Payment.Stripe.Enabled,
	})
}
