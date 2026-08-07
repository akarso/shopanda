package pluginreport

import (
	"reflect"
	"time"

	checkoutapp "github.com/akarso/shopanda/internal/application/checkout"
	exportctxapp "github.com/akarso/shopanda/internal/application/exportctx"
	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	importctxapp "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/application/ports"
	apppricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Report is a point-in-time snapshot of plugin registrations and infrastructure ports.
type Report struct {
	GeneratedAt       time.Time                   `json:"generated_at"`
	Plugins           []PluginStatus              `json:"plugins"`
	Ports             []ports.ActivePort          `json:"ports"`
	CorePricingSteps  []string                    `json:"core_pricing_steps"`
	CoreCheckoutSteps []string                    `json:"core_checkout_steps"`
	PricingSteps      []PricingStep               `json:"pricing_steps"`
	Hooks             []hooksapp.CatalogEntry     `json:"hooks"`
	ImportHooks       []importctxapp.CatalogEntry `json:"import_hooks"`
	ExportHooks       []exportctxapp.CatalogEntry `json:"export_hooks"`
	CompositionSteps  []CompositionStep           `json:"composition_steps"`
	CheckoutSteps     []CheckoutStep              `json:"checkout_steps"`
	SyncJobs          []SyncJob                   `json:"sync_jobs"`
	PublicRoutes      []Route                     `json:"public_routes"`
	AdminRoutes       []AdminRoute                `json:"admin_routes"`
}

// PluginStatus describes one plugin lifecycle entry.
type PluginStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// PricingStep describes a plugin pricing pipeline registration.
type PricingStep struct {
	Position string `json:"position"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

// CompositionStep describes a composition pipeline plugin step.
type CompositionStep struct {
	Pipeline string `json:"pipeline"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

// CheckoutStep describes a plugin checkout workflow registration.
type CheckoutStep struct {
	Position string `json:"position"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

// SyncJob describes an outbound integration sync job registration.
type SyncJob struct {
	PluginSlug string `json:"plugin_slug"`
	Name       string `json:"name"`
	JobType    string `json:"job_type"`
	Trigger    string `json:"trigger"`
	Detail     string `json:"detail,omitempty"`
	MaxRetries int    `json:"max_retries,omitempty"`
}

// Route describes a public HTTP route registration.
type Route struct {
	Pattern string `json:"pattern"`
}

// AdminRoute describes an admin HTTP route registration.
type AdminRoute struct {
	Pattern    string `json:"pattern"`
	Permission string `json:"permission"`
}

var compositionPipelines = []string{"pdp", "plp"}

// Build collects plugin registrations and infrastructure ports from registry and app.
func Build(registry *plugin.Registry, app *plugin.App, cfg *config.Config) Report {
	if app == nil {
		app = &plugin.App{}
	}
	report := Report{
		GeneratedAt:       time.Now().UTC(),
		Ports:             ports.BuildSnapshot(app, cfg).Ports,
		CorePricingSteps:  append([]string(nil), apppricing.CoreStepCatalog...),
		CoreCheckoutSteps: append([]string(nil), checkoutapp.CoreStepCatalog...),
	}
	if registry != nil {
		for _, entry := range registry.Entries() {
			status := PluginStatus{Name: entry.Name, State: string(entry.State)}
			if entry.Err != nil {
				status.Error = entry.Err.Error()
			}
			report.Plugins = append(report.Plugins, status)
		}
	}
	for _, reg := range app.PricingStepRegistrations() {
		report.PricingSteps = append(report.PricingSteps, PricingStep{
			Position: reg.Position,
			Name:     stepName(reg.Step),
			Type:     typeName(reg.Step),
		})
	}
	if hookReg := app.HookRegistry(); hookReg != nil {
		report.Hooks = hookReg.Catalog()
	}
	if importReg := app.ImportRegistry(); importReg != nil {
		report.ImportHooks = importReg.Catalog()
	}
	if exportReg := app.ExportRegistry(); exportReg != nil {
		report.ExportHooks = exportReg.Catalog()
	}
	for _, pipeline := range compositionPipelines {
		for _, step := range app.CompositionSteps(pipeline) {
			report.CompositionSteps = append(report.CompositionSteps, CompositionStep{
				Pipeline: pipeline,
				Name:     stepName(step),
				Type:     typeName(step),
			})
		}
	}
	for _, reg := range app.CheckoutStepRegistrations() {
		report.CheckoutSteps = append(report.CheckoutSteps, CheckoutStep{
			Position: reg.Position,
			Name:     stepName(reg.Step),
			Type:     typeName(reg.Step),
		})
	}
	for _, job := range app.SyncJobs() {
		entry := SyncJob{
			PluginSlug: job.PluginSlug,
			Name:       job.Job.Name,
			JobType:    job.JobType,
			Trigger:    job.Job.Trigger.Kind,
			MaxRetries: job.Job.MaxRetries,
		}
		switch job.Job.Trigger.Kind {
		case "cron":
			entry.Detail = job.Job.Trigger.CronSpec
		case "event":
			entry.Detail = job.Job.Trigger.EventName
		}
		report.SyncJobs = append(report.SyncJobs, entry)
	}
	for _, route := range app.PublicRoutes() {
		report.PublicRoutes = append(report.PublicRoutes, Route{Pattern: route.Pattern})
	}
	for _, route := range app.AdminRoutes() {
		report.AdminRoutes = append(report.AdminRoutes, AdminRoute{
			Pattern:    route.Pattern,
			Permission: string(route.Permission),
		})
	}
	return report
}

type stepNamer interface {
	Name() string
}

func stepName(v any) string {
	if v == nil {
		return ""
	}
	if n, ok := v.(stepNamer); ok {
		return n.Name()
	}
	return typeName(v)
}

func typeName(v any) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	return t.PkgPath() + "." + t.Name()
}
