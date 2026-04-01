package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ensurePricesTable runs migrations and cleans up prices + variants + products after test.
func ensurePricesTable(t *testing.T, db interface {
	Exec(string, ...interface{}) (interface{ RowsAffected() (int64, error) }, error)
}) {
	// migrations already run by ensureProductsTable; just clean up prices
}

func mustNewPrice(t *testing.T, variantID string, amount int64, currency string) pricing.Price {
	t.Helper()
	m := shared.MustNewMoney(amount, currency)
	p, err := pricing.NewPrice(id.New(), variantID, m)
	if err != nil {
		t.Fatalf("NewPrice: %v", err)
	}
	return p
}

func TestPriceRepo_UpsertAndFind(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM prices") })

	ctx := context.Background()

	// Create a product + variant to reference
	prodRepo := postgres.NewProductRepo(db)
	prod := mustNewProduct(t, "Test Product", "test-price-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo := postgres.NewVariantRepo(db)
	v := mustNewVariant(t, prod.ID, "SKU-PRICE-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo := postgres.NewPriceRepo(db)

	// Upsert a price
	price := mustNewPrice(t, v.ID, 1299, "EUR")
	if err := repo.Upsert(ctx, &price); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Find it
	found, err := repo.FindByVariantAndCurrency(ctx, v.ID, "EUR")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatal("expected price, got nil")
	}
	if found.Amount.Amount() != 1299 {
		t.Errorf("amount = %d, want 1299", found.Amount.Amount())
	}
	if found.Amount.Currency() != "EUR" {
		t.Errorf("currency = %q, want EUR", found.Amount.Currency())
	}
}

func TestPriceRepo_FindNotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	repo := postgres.NewPriceRepo(db)

	found, err := repo.FindByVariantAndCurrency(ctx, id.New(), "EUR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil, got %v", found)
	}
}

func TestPriceRepo_UpsertUpdatesAmount(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM prices") })

	ctx := context.Background()

	prodRepo := postgres.NewProductRepo(db)
	prod := mustNewProduct(t, "Test Product", "test-upsert-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo := postgres.NewVariantRepo(db)
	v := mustNewVariant(t, prod.ID, "SKU-UPSERT-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo := postgres.NewPriceRepo(db)

	// Insert initial price
	price := mustNewPrice(t, v.ID, 1000, "EUR")
	if err := repo.Upsert(ctx, &price); err != nil {
		t.Fatalf("upsert initial: %v", err)
	}

	// Upsert with new amount (same variant+currency)
	price2 := mustNewPrice(t, v.ID, 2000, "EUR")
	if err := repo.Upsert(ctx, &price2); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	found, err := repo.FindByVariantAndCurrency(ctx, v.ID, "EUR")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Amount.Amount() != 2000 {
		t.Errorf("amount = %d, want 2000", found.Amount.Amount())
	}
}

func TestPriceRepo_ListByVariantID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM prices") })

	ctx := context.Background()

	prodRepo := postgres.NewProductRepo(db)
	prod := mustNewProduct(t, "Test Product", "test-list-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo := postgres.NewVariantRepo(db)
	v := mustNewVariant(t, prod.ID, "SKU-LIST-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo := postgres.NewPriceRepo(db)

	p1 := mustNewPrice(t, v.ID, 1299, "EUR")
	p2 := mustNewPrice(t, v.ID, 1499, "USD")
	if err := repo.Upsert(ctx, &p1); err != nil {
		t.Fatalf("upsert EUR: %v", err)
	}
	if err := repo.Upsert(ctx, &p2); err != nil {
		t.Fatalf("upsert USD: %v", err)
	}

	prices, err := repo.ListByVariantID(ctx, v.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prices) != 2 {
		t.Fatalf("got %d prices, want 2", len(prices))
	}
	// Ordered by currency: EUR first, then USD
	if prices[0].Amount.Currency() != "EUR" {
		t.Errorf("first currency = %q, want EUR", prices[0].Amount.Currency())
	}
	if prices[1].Amount.Currency() != "USD" {
		t.Errorf("second currency = %q, want USD", prices[1].Amount.Currency())
	}
}

func TestPriceRepo_UpsertNil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewPriceRepo(db)
	ctx := context.Background()

	if err := repo.Upsert(ctx, nil); err == nil {
		t.Fatal("expected error for nil price")
	}
}
