package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func ensureTaxRatesTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM tax_rates")
	})
}

func mustNewTaxRate(t *testing.T, country, class string, rate int) tax.TaxRate {
	t.Helper()
	tr, err := tax.NewTaxRate(id.New(), country, class, "", rate)
	if err != nil {
		t.Fatalf("NewTaxRate: %v", err)
	}
	return tr
}

func TestTaxRateRepo_NilDB(t *testing.T) {
	_, err := postgres.NewTaxRateRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestTaxRateRepo_UpsertAndFind(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTaxRate(t, "US", "standard", 2000)
	if err := repo.Upsert(ctx, &tr); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.FindByCountryClassAndStore(ctx, "US", "standard", "")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("Find returned nil")
	}
	if got.Rate != 2000 {
		t.Errorf("Rate: got %d, want 2000", got.Rate)
	}
	if got.Country != "US" {
		t.Errorf("Country: got %q, want %q", got.Country, "US")
	}
}

func TestTaxRateRepo_Upsert_Update(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTaxRate(t, "DE", "reduced", 700)
	if err := repo.Upsert(ctx, &tr); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	origID := tr.ID

	// Upsert same tuple with different rate — should update, not insert.
	tr2 := mustNewTaxRate(t, "DE", "reduced", 500)
	if err := repo.Upsert(ctx, &tr2); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	// RETURNING id should give back the original row id.
	if tr2.ID != origID {
		t.Errorf("ID after upsert: got %q, want %q (original)", tr2.ID, origID)
	}

	got, err := repo.FindByCountryClassAndStore(ctx, "DE", "reduced", "")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Rate != 500 {
		t.Errorf("Rate after upsert: got %d, want 500", got.Rate)
	}
}

func TestTaxRateRepo_CreateIfNotExists(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTaxRate(t, "PL", "standard", 2300)
	created, err := repo.CreateIfNotExists(ctx, &tr)
	if err != nil {
		t.Fatalf("CreateIfNotExists: %v", err)
	}
	if !created {
		t.Fatal("CreateIfNotExists() = false, want true on first insert")
	}
	originalID := tr.ID

	conflict := mustNewTaxRate(t, "PL", "standard", 800)
	created, err = repo.CreateIfNotExists(ctx, &conflict)
	if err != nil {
		t.Fatalf("CreateIfNotExists conflict: %v", err)
	}
	if created {
		t.Fatal("CreateIfNotExists() = true, want false on existing tuple")
	}

	got, err := repo.FindByCountryClassAndStore(ctx, "PL", "standard", "")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("Find returned nil")
	}
	if got.ID != originalID {
		t.Errorf("ID after create-if-not-exists conflict: got %q, want %q", got.ID, originalID)
	}
	if got.Rate != 2300 {
		t.Errorf("Rate after create-if-not-exists conflict: got %d, want 2300", got.Rate)
	}
}

func TestTaxRateRepo_ListByCountry(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	tr1 := mustNewTaxRate(t, "FR", "standard", 2000)
	tr2 := mustNewTaxRate(t, "FR", "reduced", 550)
	tr3 := mustNewTaxRate(t, "GB", "standard", 2000) // different country
	for _, tr := range []*tax.TaxRate{&tr1, &tr2, &tr3} {
		if err := repo.Upsert(ctx, tr); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	list, err := repo.ListByCountry(ctx, "FR")
	if err != nil {
		t.Fatalf("ListByCountry: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByCountry FR: got %d, want 2", len(list))
	}
}

func TestTaxRateRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTaxRate(t, "IT", "standard", 2200)
	if err := repo.Upsert(ctx, &tr); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.Delete(ctx, tr.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByCountryClassAndStore(ctx, "IT", "standard", "")
	if err != nil {
		t.Fatalf("Find after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestTaxRateRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}

	err = repo.Delete(context.Background(), id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent rate")
	}
}

func TestTaxRateRepo_FindByCountryClassAndStore_NotFound(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}

	got, err := repo.FindByCountryClassAndStore(context.Background(), "ZZ", "nope", "")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent rate")
	}
}

func TestTaxRateRepo_Upsert_Nil(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}

	if err := repo.Upsert(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil rate")
	}
}

func TestTaxRateRepo_CreateIfNotExists_Nil(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}

	if _, err := repo.CreateIfNotExists(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil rate")
	}
}
