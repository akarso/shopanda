package pluginsdk_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// TestAsHostFor_ConfigValue_ScopedToOwnPlugin covers PR-1546 review fix: a
// plugin Host created via AsHostFor (what Adapt uses for every real plugin
// Init) can only read its own `plugins.<slug>.*` config namespace, not core
// secrets (database password) or another plugin's config.
func TestAsHostFor_ConfigValue_ScopedToOwnPlugin(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Password: "super-secret"},
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
			B2B:     config.B2BPluginConfig{LicenseKey: "other-plugins-license"},
		},
	}
	app := &plugin.App{Config: cfg}
	h := pluginsdk.AsHostFor(app, "example/demo")

	if got := h.ConfigValue("plugins.example.enabled"); got != "true" {
		t.Fatalf("own key: ConfigValue = %q, want true", got)
	}
	if got := h.ConfigValue("database.password"); got != "" {
		t.Fatalf("core secret leaked through ConfigValue: %q", got)
	}
	if got := h.ConfigValue("plugins.b2b.license_key"); got != "" {
		t.Fatalf("other plugin's secret leaked through ConfigValue: %q", got)
	}
}

// TestAsHost_ConfigValue_Unscoped confirms the bare AsHost (no plugin name —
// tests/harness only) keeps the pre-fix full-config behavior.
func TestAsHost_ConfigValue_Unscoped(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Password: "super-secret"}}
	app := &plugin.App{Config: cfg}
	h := pluginsdk.AsHost(app)
	if got := h.ConfigValue("database.password"); got != "super-secret" {
		t.Fatalf("ConfigValue = %q, want super-secret (AsHost is unscoped)", got)
	}
}

type scopingFakeCatalog struct{}

func (scopingFakeCatalog) FindVariant(context.Context, string) (*extapi.CatalogVariant, error) {
	return nil, nil
}
func (scopingFakeCatalog) FindVariants(context.Context, []string) (map[string]*extapi.CatalogVariant, error) {
	return nil, nil
}
func (scopingFakeCatalog) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}

// TestAsHostFor_Catalog_DeniedWithoutDeclaredNeed covers the gating half of
// the review fix: a plugin that never declared Need(CapCatalog) must not see
// the catalog port just because another plugin's Need caused the root to
// bind it.
func TestAsHostFor_Catalog_DeniedWithoutDeclaredNeed(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindCatalogPort(scopingFakeCatalog{}); err != nil {
		t.Fatalf("BindCatalogPort: %v", err)
	}
	// Plugin "other/plugin" bound (or will declare) the capability; "acme/demo" didn't.
	app.RecordNeeds(plugin.CapCatalog) // no CurrentPluginName set — attributed to nobody
	h := pluginsdk.AsHostFor(app, "acme/demo")
	if _, ok := h.Catalog(); ok {
		t.Fatal("expected Catalog() to be denied: acme/demo never declared Need(CapCatalog)")
	}
}

func TestAsHostFor_Catalog_AllowedAfterRequire(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindCatalogPort(scopingFakeCatalog{}); err != nil {
		t.Fatalf("BindCatalogPort: %v", err)
	}
	h := pluginsdk.AsHostFor(app, "acme/demo")
	h.Require(pluginsdk.CapCatalog)
	if _, ok := h.Catalog(); !ok {
		t.Fatal("expected Catalog() to be allowed after Require(CapCatalog)")
	}
}

func TestAsHostFor_BootstrapDB_DeniedWithoutDeclaredNeed(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}}}
	h := pluginsdk.AsHostFor(app, "acme/demo")
	if _, ok := h.BootstrapDB(); ok {
		t.Fatal("expected BootstrapDB() to be denied without Require(CapDB)")
	}
}

func TestAsHostFor_BootstrapDB_AllowedAfterRequire(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}}}
	h := pluginsdk.AsHostFor(app, "acme/demo")
	h.Require(pluginsdk.CapDB)
	if _, ok := h.BootstrapDB(); !ok {
		t.Fatal("expected BootstrapDB() to be allowed after Require(CapDB)")
	}
}

// TestAsHost_PortGetters_Unscoped confirms a bare AsHost Host is not gated by
// declared Needs (back-compat for tests/harness that bind ports directly).
func TestAsHost_PortGetters_Unscoped(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindCatalogPort(scopingFakeCatalog{}); err != nil {
		t.Fatalf("BindCatalogPort: %v", err)
	}
	h := pluginsdk.AsHost(app)
	if _, ok := h.Catalog(); !ok {
		t.Fatal("expected Catalog() to be allowed on an unscoped AsHost Host")
	}
}

// TestAsHostFor_ConfigSlug_ShopandaB2B: Name "shopanda/b2b" must resolve to
// plugins.b2b.* (not plugins.shopanda.*) so Adapt Init sees enabled/license.
func TestAsHostFor_ConfigSlug_ShopandaB2B(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-local"},
		},
	}
	h := pluginsdk.AsHostFor(&plugin.App{Config: cfg}, "shopanda/b2b")
	if !h.ConfigBool("plugins.b2b.enabled") {
		t.Fatal("expected plugins.b2b.enabled=true via shopanda/b2b Host")
	}
	if got := h.ConfigValue("plugins.b2b.license_key"); got != "DEV-local" {
		t.Fatalf("license_key = %q, want DEV-local", got)
	}
	if got := h.ConfigValue("plugins.shopanda.enabled"); got != "" {
		t.Fatalf("unexpected plugins.shopanda.* read: %q", got)
	}
}

// TestAsHostFor_ConfigSlug_ReferenceUsesFirstSegment: slug/reference and
// example/demo keep the first path segment; vendor/product names use last.
func TestAsHostFor_ConfigSlug_ReferenceUsesFirstSegment(t *testing.T) {
	href := pluginsdk.AsHostFor(&plugin.App{Config: &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{Enabled: true},
		},
	}}, "cartdemo/reference")
	if !href.ConfigBool("plugins.cartdemo.enabled") {
		t.Fatal("expected plugins.cartdemo.enabled for cartdemo/reference")
	}
	if got := href.ConfigValue("plugins.reference.enabled"); got != "" {
		t.Fatalf("unexpected plugins.reference.* read: %q", got)
	}
}

type scopingFakeCart struct{}

func (scopingFakeCart) AddItem(context.Context, string, string, string, int, pluginsdk.CartAddItemOpts) error {
	return nil
}
func (scopingFakeCart) Get(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (scopingFakeCart) EnsureActive(context.Context, string, string) (*extapi.CartView, error) {
	return nil, nil
}
func (scopingFakeCart) EnsureStorefrontCart(context.Context, http.ResponseWriter, *http.Request, string) (*extapi.CartView, error) {
	return nil, nil
}
func (scopingFakeCart) DiscardEmptyGuestCart(context.Context, http.ResponseWriter, *http.Request) error {
	return nil
}
func (scopingFakeCart) DeleteLineExtension(context.Context, string, string, string, string) error {
	return nil
}
func (scopingFakeCart) UpsertLineExtension(context.Context, string, string, string, []pluginsdk.CartExtensionValue, string) error {
	return nil
}

func TestAsHostFor_Cart_DeniedWithoutDeclaredNeed(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindCartPort(scopingFakeCart{}); err != nil {
		t.Fatalf("BindCartPort: %v", err)
	}
	h := pluginsdk.AsHostFor(app, "acme/demo")
	if _, ok := h.Cart(); ok {
		t.Fatal("expected Cart() denied without Require(CapCart)")
	}
}

func TestAsHostFor_Cart_NonCartPortDeniedWhenAttributed(t *testing.T) {
	app := &plugin.App{}
	if err := app.BindCartPort(scopingFakeCart{}); err != nil {
		t.Fatalf("BindCartPort: %v", err)
	}
	h := pluginsdk.AsHostFor(app, "acme/demo")
	h.Require(pluginsdk.CapCart)
	// Fake CartPort cannot be owner-scoped — fail closed instead of returning unscoped.
	if _, ok := h.Cart(); ok {
		t.Fatal("expected Cart() denied for non-*cartPort binding on attributed Host")
	}
}
