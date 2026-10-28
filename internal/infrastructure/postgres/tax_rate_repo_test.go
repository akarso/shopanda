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
	return mustNewTaxRateStore(t, country, class, "", rate)
}

func mustNewTaxRateStore(t *testing.T, country, class, storeID string, rate int) tax.TaxRate {
	t.Helper()
	tr, err := tax.NewTaxRate(id.New(), country, class, storeID, rate)
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

func TestTaxRateRepo_ListByClass(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	// Two countries × two store scopes for digital; noise from other classes.
	rows := []tax.TaxRate{
		mustNewTaxRateStore(t, "DE", tax.TaxClassDigital, "", 1900),
		mustNewTaxRateStore(t, "DE", tax.TaxClassDigital, "store-a", 1600),
		mustNewTaxRateStore(t, "FR", tax.TaxClassDigital, "", 2000),
		mustNewTaxRateStore(t, "FR", tax.TaxClassDigital, "store-b", 2100),
		mustNewTaxRate(t, "DE", tax.TaxClassStandard, 1900),
		mustNewTaxRate(t, "FR", tax.TaxClassReduced, 550),
	}
	for i := range rows {
		if err := repo.Upsert(ctx, &rows[i]); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	list, err := repo.ListByClass(ctx, tax.TaxClassDigital)
	if err != nil {
		t.Fatalf("ListByClass: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("ListByClass digital: got %d, want 4", len(list))
	}
	want := []struct {
		country, store string
		rate           int
	}{
		{"DE", "", 1900},
		{"DE", "store-a", 1600},
		{"FR", "", 2000},
		{"FR", "store-b", 2100},
	}
	for i, w := range want {
		if list[i].Country != w.country || list[i].StoreID != w.store || list[i].Rate != w.rate {
			t.Errorf("list[%d] = {%s,%s,%d}, want {%s,%s,%d}",
				i, list[i].Country, list[i].StoreID, list[i].Rate, w.country, w.store, w.rate)
		}
		if list[i].Class != tax.TaxClassDigital {
			t.Errorf("list[%d].Class = %q, want digital", i, list[i].Class)
		}
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

// TestTaxRateRepo_Migration108StyleInsert mirrors migrations/108: copy every
// non-zero standard rate into digital without clobbering an existing digital row
// or inventing digital from zero-bps standard.
func TestTaxRateRepo_Migration108StyleInsert(t *testing.T) {
	db := testDB(t)
	ensureTaxRatesTable(t, db)
	mustExec(t, db, "DELETE FROM tax_rates")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM tax_rates") })

	repo, err := postgres.NewTaxRateRepo(db)
	if err != nil {
		t.Fatalf("NewTaxRateRepo: %v", err)
	}
	ctx := context.Background()

	stdDE := mustNewTaxRate(t, "DE", tax.TaxClassStandard, 1900)
	stdFR := mustNewTaxRateStore(t, "FR", tax.TaxClassStandard, "store-1", 2000)
	stdLU := mustNewTaxRate(t, "LU", tax.TaxClassStandard, 0)
	existingDigital := mustNewTaxRate(t, "DE", tax.TaxClassDigital, 700)
	for _, r := range []*tax.TaxRate{&stdDE, &stdFR, &stdLU, &existingDigital} {
		if err := repo.Upsert(ctx, r); err != nil {
			t.Fatalf("Upsert setup: %v", err)
		}
	}

	mustExec(t, db, `
INSERT INTO tax_rates (id, country, class, store_id, rate)
SELECT gen_random_uuid(), country, 'digital', store_id, rate
FROM tax_rates
WHERE class = 'standard'
  AND rate > 0
ON CONFLICT (country, class, store_id) DO NOTHING`)

	gotDE, err := repo.FindByCountryClassAndStore(ctx, "DE", tax.TaxClassDigital, "")
	if err != nil {
		t.Fatalf("Find DE digital: %v", err)
	}
	if gotDE == nil || gotDE.Rate != 700 {
		t.Fatalf("DE digital clobbered: %+v", gotDE)
	}
	gotFR, err := repo.FindByCountryClassAndStore(ctx, "FR", tax.TaxClassDigital, "store-1")
	if err != nil {
		t.Fatalf("Find FR digital: %v", err)
	}
	if gotFR == nil || gotFR.Rate != 2000 {
		t.Fatalf("FR digital = %+v, want rate 2000", gotFR)
	}
	gotLU, err := repo.FindByCountryClassAndStore(ctx, "LU", tax.TaxClassDigital, "")
	if err != nil {
		t.Fatalf("Find LU digital: %v", err)
	}
	if gotLU != nil {
		t.Fatalf("zero-bps standard must not create digital, got %+v", gotLU)
	}
}
