package b2b_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b"
)

func TestPlugin_Init_MissingCustomersReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-local"},
		},
	}
	app := testApp(cfg, nil)
	app.Bootstrap = &plugin.Bootstrap{
		DB:       &sql.DB{},
		Variants: &wireStubVariantRepo{},
	}
	err := b2b.New().Init(app)
	if err == nil || !strings.Contains(err.Error(), "customer repository not configured") {
		t.Fatalf("Init() error = %v, want customer repository not configured", err)
	}
}

func TestPlugin_Init_MissingVariantsReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-local"},
		},
	}
	app := testApp(cfg, nil)
	app.Bootstrap = &plugin.Bootstrap{
		DB:        &sql.DB{},
		Customers: &wireStubCustomerRepo{},
	}
	err := b2b.New().Init(app)
	if err == nil || !strings.Contains(err.Error(), "variant repository not configured") {
		t.Fatalf("Init() error = %v, want variant repository not configured", err)
	}
}

func TestNewAdminHandlers_UsesBootstrapRepos(t *testing.T) {
	customers := &wireStubCustomerRepo{label: "customers"}
	variants := &wireStubVariantRepo{label: "variants"}
	boot := &plugin.Bootstrap{Customers: customers, Variants: variants}

	groupAdmin, priceAdmin := b2b.NewAdminHandlersForTest(boot, &wireStubGroupRepo{}, &wireStubPriceRepo{})
	if groupAdmin.CustomerRepository() != customers {
		t.Fatal("group admin did not receive Bootstrap.Customers")
	}
	if priceAdmin.VariantRepository() != variants {
		t.Fatal("price admin did not receive Bootstrap.Variants")
	}
}

// Stubs below exist only to satisfy constructor nil-checks / interface wiring tests.

type wireStubCustomerRepo struct{ label string }

func (s *wireStubCustomerRepo) FindByID(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (s *wireStubCustomerRepo) FindByEmail(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (s *wireStubCustomerRepo) Create(context.Context, *customer.Customer) error { return nil }
func (s *wireStubCustomerRepo) Update(context.Context, *customer.Customer) error { return nil }
func (s *wireStubCustomerRepo) ListCustomers(context.Context, int, int) ([]customer.Customer, error) {
	return nil, nil
}
func (s *wireStubCustomerRepo) BumpTokenGeneration(context.Context, string) error { return nil }
func (s *wireStubCustomerRepo) ChangePasswordAndBumpTokenGeneration(context.Context, string, string) error {
	return nil
}
func (s *wireStubCustomerRepo) Delete(context.Context, string) error { return nil }

type wireStubVariantRepo struct{ label string }

func (s *wireStubVariantRepo) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (s *wireStubVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (s *wireStubVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (s *wireStubVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (s *wireStubVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (s *wireStubVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (s *wireStubVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type wireStubGroupRepo struct{}

func (wireStubGroupRepo) List(context.Context, int, int) ([]customergroup.Group, error) {
	return nil, nil
}
func (wireStubGroupRepo) FindByID(context.Context, string) (*customergroup.Group, error) {
	return nil, nil
}
func (wireStubGroupRepo) FindByCode(context.Context, string) (*customergroup.Group, error) {
	return nil, nil
}
func (wireStubGroupRepo) Save(context.Context, *customergroup.Group) error { return nil }
func (wireStubGroupRepo) Delete(context.Context, string) error             { return nil }
func (wireStubGroupRepo) AssignCustomer(context.Context, string, string) error {
	return nil
}
func (wireStubGroupRepo) RemoveCustomer(context.Context, string) error { return nil }
func (wireStubGroupRepo) FindGroupByCustomerID(context.Context, string) (*customergroup.Group, error) {
	return nil, nil
}

type wireStubPriceRepo struct{}

func (wireStubPriceRepo) FindByVariantsGroupCurrencyAndStore(context.Context, []string, string, string, string) (map[string]*customergroup.GroupPrice, error) {
	return nil, nil
}
func (wireStubPriceRepo) FindExactByVariantGroupCurrencyAndStore(context.Context, string, string, string, string) (*customergroup.GroupPrice, error) {
	return nil, nil
}
func (wireStubPriceRepo) FindByVariantGroupCurrencyAndStore(context.Context, string, string, string, string) (*customergroup.GroupPrice, error) {
	return nil, nil
}
func (wireStubPriceRepo) Upsert(context.Context, *customergroup.GroupPrice) error { return nil }
func (wireStubPriceRepo) Delete(context.Context, string) error                    { return nil }
