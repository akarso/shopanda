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
	return mustNewPriceWithStore(t, variantID, "", amount, currency)
}

func mustNewPriceWithStore(t *testing.T, variantID, storeID string, amount int64, currency string) pricing.Price {
	t.Helper()
	m := shared.MustNewMoney(amount, currency)
	p, err := pricing.NewPrice(id.New(), variantID, storeID, m)
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
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Test Product", "test-price-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-PRICE-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

	// Upsert a price
	price := mustNewPrice(t, v.ID, 1299, "EUR")
	if err := repo.Upsert(ctx, &price); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Find it
	found, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "")
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
	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

	found, err := repo.FindByVariantCurrencyAndStore(ctx, id.New(), "EUR", "")
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

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Test Product", "test-upsert-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-UPSERT-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

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

	found, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "")
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

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Test Product", "test-list-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-LIST-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

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

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.Upsert(ctx, nil); err == nil {
		t.Fatal("expected error for nil price")
	}
}

func TestPriceRepo_StoreScopedUpsertAndFind(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM prices") })

	ctx := context.Background()

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Test Product", "test-store-price-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-STOREPRICE-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

	// Upsert a store-scoped price.
	price := mustNewPriceWithStore(t, v.ID, "store-de", 899, "EUR")
	if err := repo.Upsert(ctx, &price); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Find it by store.
	found, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "store-de")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatal("expected store-scoped price, got nil")
	}
	if found.Amount.Amount() != 899 {
		t.Errorf("amount = %d, want 899", found.Amount.Amount())
	}
	if found.StoreID != "store-de" {
		t.Errorf("storeID = %q, want store-de", found.StoreID)
	}

	// Global lookup should NOT return the store-scoped price.
	global, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "")
	if err != nil {
		t.Fatalf("find global: %v", err)
	}
	if global != nil {
		t.Error("expected nil for global lookup, got a price")
	}
}

func TestPriceRepo_StoreScopedCoexistence(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM prices") })

	ctx := context.Background()

	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	prod := mustNewProduct(t, "Test Product", "test-coexist-"+id.New()[:8])
	if err := prodRepo.Create(ctx, &prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variantRepo, err := postgres.NewVariantRepo(db)
	if err != nil {
		t.Fatalf("NewVariantRepo: %v", err)
	}
	v := mustNewVariant(t, prod.ID, "SKU-COEXIST-"+id.New()[:8])
	if err := variantRepo.Create(ctx, &v); err != nil {
		t.Fatalf("create variant: %v", err)
	}

	repo, err := postgres.NewPriceRepo(db)
	if err != nil {
		t.Fatalf("NewPriceRepo: %v", err)
	}

	// Global price.
	global := mustNewPrice(t, v.ID, 1000, "EUR")
	if err := repo.Upsert(ctx, &global); err != nil {
		t.Fatalf("upsert global: %v", err)
	}
	// Store-scoped price (same variant + currency, different store).
	scoped := mustNewPriceWithStore(t, v.ID, "store-us", 799, "EUR")
	if err := repo.Upsert(ctx, &scoped); err != nil {
		t.Fatalf("upsert scoped: %v", err)
	}

	// Both coexist.
	foundGlobal, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "")
	if err != nil {
		t.Fatalf("find global: %v", err)
	}
	if foundGlobal == nil || foundGlobal.Amount.Amount() != 1000 {
		t.Errorf("global amount = %v, want 1000", foundGlobal)
	}
	foundScoped, err := repo.FindByVariantCurrencyAndStore(ctx, v.ID, "EUR", "store-us")
	if err != nil {
		t.Fatalf("find scoped: %v", err)
	}
	if foundScoped == nil || foundScoped.Amount.Amount() != 799 {
		t.Errorf("scoped amount = %v, want 799", foundScoped)
	}
}
