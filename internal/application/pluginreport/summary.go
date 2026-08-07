package pluginreport

import (
	"github.com/akarso/shopanda/internal/platform/logger"
)

// LogSummary logs registration counts at startup after plugin init.
func LogSummary(log logger.Logger, report Report) {
	if log == nil {
		return
	}
	active, failed := 0, 0
	for _, p := range report.Plugins {
		switch p.State {
		case "active":
			active++
		case "failed":
			failed++
		}
	}
	hookHandlers, importHandlers, exportHandlers := 0, 0, 0
	for _, entry := range report.Hooks {
		hookHandlers += len(entry.Handlers)
	}
	for _, entry := range report.ImportHooks {
		importHandlers += len(entry.Handlers)
	}
	for _, entry := range report.ExportHooks {
		exportHandlers += len(entry.Handlers)
	}
	log.Info("plugin.registration.summary", map[string]interface{}{
		"plugins_active":       active,
		"plugins_failed":       failed,
		"pricing_steps":        len(report.PricingSteps),
		"hooks":                hookHandlers,
		"import_row_hooks":     importHandlers,
		"export_row_hooks":     exportHandlers,
		"composition_steps":    len(report.CompositionSteps),
		"checkout_steps":       len(report.CheckoutSteps),
		"sync_jobs":            len(report.SyncJobs),
		"public_routes":        len(report.PublicRoutes),
		"admin_routes":         len(report.AdminRoutes),
		"infrastructure_ports": len(report.Ports),
	})
}
