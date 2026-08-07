package ports

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Status describes runtime port availability.
type Status string

const (
	StatusActive       Status = "active"
	StatusCoreDefault  Status = "core_default"
	StatusPlanned      Status = "planned"
	StatusUnconfigured Status = "unconfigured"
)

// ProviderDetail holds one implementation binding (used for multi-provider ports).
type ProviderDetail struct {
	Key            string `json:"key,omitempty"`
	Implementation string `json:"implementation"`
	Source         string `json:"source"`
}

// ActivePort is the runtime view of a catalog entry.
type ActivePort struct {
	Name           string           `json:"name"`
	RegisterAPI    string           `json:"register_api"`
	ConfigKey      string           `json:"config_key,omitempty"`
	Status         Status           `json:"status"`
	Source         string           `json:"source,omitempty"`
	Driver         string           `json:"driver,omitempty"`
	Implementation string           `json:"implementation,omitempty"`
	Notes          string           `json:"notes,omitempty"`
	Providers      []ProviderDetail `json:"providers,omitempty"`
}

// Snapshot is a point-in-time report of infrastructure ports.
type Snapshot struct {
	Ports []ActivePort
}

// BuildSnapshot inspects plugin registration and config to produce a port report.
// It mirrors resolution rules in cmd/api/providers.go without requiring DB handles.
// app and cfg may be nil; nil app is treated as an empty plugin.App.
func BuildSnapshot(app *plugin.App, cfg *config.Config) Snapshot {
	if app == nil {
		app = &plugin.App{}
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	out := make([]ActivePort, 0, len(Catalog()))
	for _, entry := range Catalog() {
		out = append(out, buildActivePort(app, cfg, entry))
	}
	return Snapshot{Ports: out}
}

func buildActivePort(app *plugin.App, cfg *config.Config, entry CatalogEntry) ActivePort {
	port := ActivePort{
		Name:        entry.Name,
		RegisterAPI: entry.RegisterAPI,
		ConfigKey:   entry.ConfigKey,
		Notes:       entry.Notes,
	}
	if !entry.Shipped {
		port.Status = StatusPlanned
		return port
	}

	switch entry.Name {
	case "search":
		provider, _ := app.SearchProvider()
		return buildSingleProviderPort(port, provider, cfg.Search.Engine, coreSearchFallback(cfg))
	case "cache":
		provider, _ := app.Cache()
		return buildSingleProviderPort(port, provider, cfg.Cache.Driver, coreCacheFallback(cfg))
	case "queue":
		provider, _ := app.Queue()
		return buildSingleProviderPort(port, provider, cfg.Queue.Driver, coreQueueFallback(cfg))
	case "media":
		provider, _ := app.MediaStorage()
		return buildSingleProviderPort(port, provider, cfg.Media.Storage, coreMediaFallback(cfg))
	case "tax":
		provider, _ := app.TaxCalculator()
		return buildSingleProviderPort(port, provider, "", coreTaxFallback())
	case "shipping_rate":
		return buildShippingPort(port, app)
	case "mail_sender":
		provider, _ := app.MailSender()
		return buildSingleProviderPort(port, provider, cfg.Mail.Driver, coreMailFallback(cfg))
	case "payment":
		return buildPaymentPort(port, app)
	default:
		port.Status = StatusPlanned
		return port
	}
}

func buildSingleProviderPort(port ActivePort, provider any, driver string, coreImpl string) ActivePort {
	port.Driver = driver
	if provider != nil {
		port.Status = StatusActive
		port.Source = "plugin"
		port.Implementation = typeName(provider)
		return port
	}
	if coreImpl != "" {
		port.Status = StatusCoreDefault
		port.Source = "core"
		port.Implementation = coreImpl
		return port
	}
	port.Status = StatusUnconfigured
	port.Source = "core"
	return port
}

func buildPaymentPort(port ActivePort, app *plugin.App) ActivePort {
	reg := app.PaymentRegistry()
	if reg == nil || reg.Len() == 0 {
		port.Status = StatusCoreDefault
		port.Source = "core"
		port.Providers = []ProviderDetail{{
			Key:            string(payment.MethodManual),
			Implementation: "manualpay.Provider",
			Source:         "core",
		}}
		return port
	}

	providers := make([]ProviderDetail, 0, reg.Len())
	pluginRegistered := false
	for _, method := range reg.Methods() {
		p, ok := reg.Get(method)
		if !ok {
			continue
		}
		source := paymentProviderSource(p)
		if source == "plugin" {
			pluginRegistered = true
		}
		providers = append(providers, ProviderDetail{
			Key:            string(method),
			Implementation: typeName(p),
			Source:         source,
		})
	}
	port.Providers = providers
	if pluginRegistered {
		port.Status = StatusActive
		port.Source = "plugin"
	} else {
		port.Status = StatusCoreDefault
		port.Source = "core"
	}
	return port
}

func buildShippingPort(port ActivePort, app *plugin.App) ActivePort {
	reg := app.ShippingRegistry()
	if reg == nil || reg.Len() == 0 {
		port.Status = StatusCoreDefault
		port.Source = "core"
		port.Providers = []ProviderDetail{{
			Key:            string(shipping.MethodFlatRate),
			Implementation: "flatrate.Provider",
			Source:         "core",
		}}
		return port
	}

	providers := make([]ProviderDetail, 0, reg.Len())
	pluginRegistered := false
	for _, p := range reg.Providers() {
		source := shippingProviderSource(p)
		if source == "plugin" {
			pluginRegistered = true
		}
		providers = append(providers, ProviderDetail{
			Key:            string(p.Method()),
			Implementation: typeName(p),
			Source:         source,
		})
	}
	port.Providers = providers
	if pluginRegistered {
		port.Status = StatusActive
		port.Source = "plugin"
	} else {
		port.Status = StatusCoreDefault
		port.Source = "core"
	}
	return port
}

func shippingProviderSource(p shipping.Provider) string {
	name := typeName(p)
	if strings.HasPrefix(name, "flatrate.") {
		return "core"
	}
	return "plugin"
}

func paymentProviderSource(p payment.Provider) string {
	name := typeName(p)
	if strings.HasPrefix(name, "manualpay.") || strings.HasPrefix(name, "stripepay.") {
		return "core"
	}
	return "plugin"
}

func coreSearchFallback(cfg *config.Config) string {
	if cfg.Search.Engine == "postgres" {
		return "postgres.SearchEngine"
	}
	return ""
}

func coreCacheFallback(cfg *config.Config) string {
	if cfg.Cache.Driver == "postgres" {
		return "postgres.CacheStore"
	}
	return ""
}

func coreQueueFallback(cfg *config.Config) string {
	if cfg.Queue.Driver == "postgres" {
		return "postgres.JobQueue"
	}
	return ""
}

func coreMediaFallback(cfg *config.Config) string {
	if cfg.Media.Storage == "local" {
		return "localfs.Storage"
	}
	return ""
}

func coreTaxFallback() string {
	return "pricing.RateTableCalculator"
}

func coreMailFallback(cfg *config.Config) string {
	if cfg.Mail.Driver == "smtp" || cfg.Mail.Driver == "" {
		return "smtp.Mailer"
	}
	return ""
}

func typeName(v any) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	pkg := t.PkgPath()
	if idx := strings.LastIndex(pkg, "/"); idx >= 0 {
		pkg = pkg[idx+1:]
	}
	return fmt.Sprintf("%s.%s", pkg, t.Name())
}
