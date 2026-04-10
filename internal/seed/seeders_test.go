package seed

import (
	"context"
	"strings"
	"testing"
)

// Compile-time interface checks.
var (
	_ Seeder = (*AdminSeeder)(nil)
	_ Seeder = (*ConfigSeeder)(nil)
	_ Seeder = (*CatalogSeeder)(nil)
)

type noopLogger struct{}

func (noopLogger) Info(_ string, _ map[string]interface{})           {}
func (noopLogger) Warn(_ string, _ map[string]interface{})           {}
func (noopLogger) Error(_ string, _ error, _ map[string]interface{}) {}

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

func TestSeedersRegistration(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ConfigSeeder{})
	reg.Register(&AdminSeeder{})
	reg.Register(&CatalogSeeder{})

	// Verify no duplicate-name panic occurred and all three are registered.
	// Run with a nil DB to confirm the registry accepted them.
	if len(reg.seeders) != 3 {
		t.Fatalf("expected 3 registered seeders, got %d", len(reg.seeders))
	}
	names := []string{"store-config", "admin-user", "catalog"}
	for i, want := range names {
		if got := reg.seeders[i].Name(); got != want {
			t.Errorf("seeder[%d].Name() = %q, want %q", i, got, want)
		}
	}
}

func TestAdminSeeder_RequiresPasswordEnv(t *testing.T) {
	t.Setenv(adminPasswordEnvKey, "")

	s := &AdminSeeder{}
	err := s.Seed(context.Background(), Deps{Logger: &noopLogger{}})
	if err == nil {
		t.Fatal("expected error when SHOPANDA_SEED_ADMIN_PASSWORD is unset")
	}
	if !strings.Contains(err.Error(), adminPasswordEnvKey) {
		t.Errorf("error should mention %s, got: %v", adminPasswordEnvKey, err)
	}
}

func TestDefaultCategories(t *testing.T) {
	if len(defaultCategories) != 2 {
		t.Fatalf("expected 2 default categories, got %d", len(defaultCategories))
	}
	slugs := map[string]bool{"electronics": true, "clothing": true}
	for _, c := range defaultCategories {
		if !slugs[c.Slug] {
			t.Errorf("unexpected category slug %q", c.Slug)
		}
		if c.Name == "" {
			t.Errorf("category with slug %q has empty name", c.Slug)
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
