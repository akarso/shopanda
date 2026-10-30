package seed

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/tax"
)

// Compile-time interface checks.
var (
	_ Seeder = (*AdminSeeder)(nil)
	_ Seeder = (*ConfigSeeder)(nil)
	_ Seeder = (*CatalogSeeder)(nil)
	_ Seeder = (*StoreSeeder)(nil)
	_ Seeder = (*TaxSeeder)(nil)
)

func TestAdminSeeder_Name(t *testing.T) {
	s := &AdminSeeder{}
	if got := s.Name(); got != "admin-user" {
		t.Fatalf("AdminSeeder.Name() = %q, want %q", got, "admin-user")
	}
}

func TestConfigSeeder_Name(t *testing.T) {
	s := &ConfigSeeder{}
	if got := s.Name(); got != "store-config" {
		t.Fatalf("ConfigSeeder.Name() = %q, want %q", got, "store-config")
	}
}

func TestCatalogSeeder_Name(t *testing.T) {
	s := &CatalogSeeder{}
	if got := s.Name(); got != "catalog" {
		t.Fatalf("CatalogSeeder.Name() = %q, want %q", got, "catalog")
	}
}

func TestStoreSeeder_Name(t *testing.T) {
	s := &StoreSeeder{}
	if got := s.Name(); got != "default-store" {
		t.Fatalf("StoreSeeder.Name() = %q, want %q", got, "default-store")
	}
}

func TestTaxSeeder_Name(t *testing.T) {
	s := &TaxSeeder{}
	if got := s.Name(); got != "default-tax" {
		t.Fatalf("TaxSeeder.Name() = %q, want %q", got, "default-tax")
	}
}

type silentSeedLogger struct{}

func (silentSeedLogger) Info(string, map[string]interface{})         {}
func (silentSeedLogger) Warn(string, map[string]interface{})         {}
func (silentSeedLogger) Error(string, error, map[string]interface{}) {}

type stubTaxRateRepo struct {
	byClass map[string][]tax.TaxRate
	rows    map[string]tax.TaxRate // key country|class|store
}

func (s *stubTaxRateRepo) key(country, class, storeID string) string {
	return country + "|" + class + "|" + storeID
}

func (s *stubTaxRateRepo) FindByCountryClassAndStore(_ context.Context, country, class, storeID string) (*tax.TaxRate, error) {
	if s.rows == nil {
		return nil, nil
	}
	r, ok := s.rows[s.key(country, class, storeID)]
	if !ok {
		return nil, nil
	}
	cp := r
	return &cp, nil
}

func (s *stubTaxRateRepo) ListByCountry(context.Context, string) ([]tax.TaxRate, error) {
	return nil, nil
}

func (s *stubTaxRateRepo) ListByClass(_ context.Context, class string) ([]tax.TaxRate, error) {
	out := append([]tax.TaxRate(nil), s.byClass[class]...)
	return out, nil
}

func (s *stubTaxRateRepo) Upsert(context.Context, *tax.TaxRate) error { return nil }

func (s *stubTaxRateRepo) CreateIfNotExists(_ context.Context, tr *tax.TaxRate) (bool, error) {
	if s.rows == nil {
		s.rows = map[string]tax.TaxRate{}
	}
	k := s.key(tr.Country, tr.Class, tr.StoreID)
	if existing, ok := s.rows[k]; ok {
		// Do not clobber — return false and leave rate untouched.
		*tr = existing
		return false, nil
	}
	s.rows[k] = *tr
	s.byClass[tr.Class] = append(s.byClass[tr.Class], *tr)
	return true, nil
}

func (s *stubTaxRateRepo) Delete(context.Context, string) error { return nil }

var _ tax.RateRepository = (*stubTaxRateRepo)(nil)

func TestBackfillDigitalFromStandard_CreatesWithoutClobber(t *testing.T) {
	existingDigital := tax.TaxRate{
		ID: "keep-me", Country: "DE", Class: tax.TaxClassDigital, StoreID: "", Rate: 9999,
	}
	w := &stubTaxRateRepo{
		byClass: map[string][]tax.TaxRate{
			tax.TaxClassStandard: {
				{ID: "s-de", Country: "DE", Class: tax.TaxClassStandard, StoreID: "", Rate: 1900},
				{ID: "s-fr", Country: "FR", Class: tax.TaxClassStandard, StoreID: "store-1", Rate: 2000},
				{ID: "s-lu", Country: "LU", Class: tax.TaxClassStandard, StoreID: "", Rate: 0},
			},
		},
		rows: map[string]tax.TaxRate{
			"DE|digital|": existingDigital,
		},
	}
	log := silentSeedLogger{}
	if err := backfillDigitalFromStandard(context.Background(), w, log); err != nil {
		t.Fatalf("backfillDigitalFromStandard: %v", err)
	}

	gotDE := w.rows["DE|digital|"]
	if gotDE.ID != "keep-me" || gotDE.Rate != 9999 {
		t.Fatalf("DE digital clobbered: %+v", gotDE)
	}
	gotFR, ok := w.rows["FR|digital|store-1"]
	if !ok {
		t.Fatal("expected FR digital row for store-1")
	}
	if gotFR.Rate != 2000 {
		t.Errorf("FR digital rate = %d, want 2000", gotFR.Rate)
	}
	if gotFR.Class != tax.TaxClassDigital {
		t.Errorf("FR class = %q, want digital", gotFR.Class)
	}
	if _, ok := w.rows["LU|digital|"]; ok {
		t.Fatal("zero-bps standard must not create digital row")
	}
}

func TestSeedersRegistration(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ConfigSeeder{})
	reg.Register(&StoreSeeder{})
	reg.Register(&TaxSeeder{})
	reg.Register(&AdminSeeder{})
	reg.Register(&CatalogSeeder{})

	// Verify no duplicate-name panic occurred and all seeders are registered.
	// Run with a nil DB to confirm the registry accepted them.
	if len(reg.seeders) != 5 {
		t.Fatalf("expected 5 registered seeders, got %d", len(reg.seeders))
	}
	names := []string{"store-config", "default-store", "default-tax", "admin-user", "catalog"}
	for i, want := range names {
		if got := reg.seeders[i].Name(); got != want {
			t.Errorf("seeder[%d].Name() = %q, want %q", i, got, want)
		}
	}
}

func TestDefaultCategories(t *testing.T) {
	if len(defaultCategories) != 2 {
		t.Fatalf("expected 2 default categories, got %d", len(defaultCategories))
	}
	seen := make(map[string]int)
	for _, c := range defaultCategories {
		seen[c.Slug]++
		if c.Name == "" {
			t.Errorf("category with slug %q has empty name", c.Slug)
		}
	}
	for _, slug := range []string{"electronics", "clothing"} {
		if n := seen[slug]; n != 1 {
			t.Errorf("expected slug %q exactly once, got %d", slug, n)
		}
	}
}

func TestDefaultProducts(t *testing.T) {
	if len(defaultProducts) != 3 {
		t.Fatalf("expected 3 default products, got %d", len(defaultProducts))
	}

	skus := make(map[string]bool)
	for _, p := range defaultProducts {
		if p.Name == "" {
			t.Error("product has empty name")
		}
		if p.Slug == "" {
			t.Error("product has empty slug")
		}
		if len(p.Variants) == 0 {
			t.Errorf("product %q has no variants", p.Slug)
		}
		for _, v := range p.Variants {
			if v.SKU == "" {
				t.Errorf("variant in product %q has empty SKU", p.Slug)
			}
			if skus[v.SKU] {
				t.Errorf("duplicate variant SKU %q", v.SKU)
			}
			skus[v.SKU] = true
			if v.Amount <= 0 {
				t.Errorf("variant %q has non-positive amount %d", v.SKU, v.Amount)
			}
			if v.Currency == "" {
				t.Errorf("variant %q has empty currency", v.SKU)
			}
			if v.Stock < 0 {
				t.Errorf("variant %q has negative stock %d", v.SKU, v.Stock)
			}
		}
	}
}
