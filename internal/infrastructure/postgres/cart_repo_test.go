package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewCart(t *testing.T, currency string) cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), currency)
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	return c
}

func TestCartRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "EUR")
	price := shared.MustNewMoney(1500, "EUR")
	if err := c.AddItem("var-1", 2, price); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID = %q, want %q", got.ID, c.ID)
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
	if got.Status() != cart.CartStatusActive {
		t.Errorf("Status = %q, want active", got.Status())
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	if got.Items[0].VariantID != "var-1" {
		t.Errorf("Items[0].VariantID = %q, want var-1", got.Items[0].VariantID)
	}
	if got.Items[0].Quantity != 2 {
		t.Errorf("Items[0].Quantity = %d, want 2", got.Items[0].Quantity)
	}
	if got.Items[0].UnitPrice.Amount() != 1500 {
		t.Errorf("Items[0].UnitPrice = %d, want 1500", got.Items[0].UnitPrice.Amount())
	}
}

func TestCartRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewCartRepo(db)
	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent cart")
	}
}

func TestCartRepo_FindActiveByCustomerID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "USD")
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}

	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindActiveByCustomerID(ctx, "cust-1")
	if err != nil {
		t.Fatalf("FindActiveByCustomerID: %v", err)
	}
	if got == nil {
		t.Fatal("expected cart, got nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID = %q, want %q", got.ID, c.ID)
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", got.CustomerID)
	}
}

func TestCartRepo_FindActiveByCustomerID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewCartRepo(db)
	got, err := repo.FindActiveByCustomerID(context.Background(), "no-customer")
	if err != nil {
		t.Fatalf("FindActiveByCustomerID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent customer cart")
	}
}

func TestCartRepo_Save_UpdateItems(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "EUR")
	price := shared.MustNewMoney(1000, "EUR")
	if err := c.AddItem("var-1", 1, price); err != nil {
		t.Fatalf("AddItem var-1: %v", err)
	}
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Add another item and re-save.
	price2 := shared.MustNewMoney(2000, "EUR")
	if err := c.AddItem("var-2", 3, price2); err != nil {
		t.Fatalf("AddItem var-2: %v", err)
	}
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(got.Items))
	}
}

func TestCartRepo_Save_EmptyCart(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "EUR")
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected cart, got nil")
	}
	if len(got.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(got.Items))
	}
}

func TestCartRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "EUR")
	price := shared.MustNewMoney(500, "EUR")
	if err := c.AddItem("var-1", 1, price); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := repo.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestCartRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewCartRepo(db)

	err := repo.Save(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestCartRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewCartRepo(db)

	_, err := repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCartRepo_Delete_EmptyID(t *testing.T) {
	db := testDB(t)
	repo := postgres.NewCartRepo(db)

	err := repo.Delete(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCartRepo_Save_WithStatus(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM cart_items")
		db.Exec("DELETE FROM carts")
	})

	repo := postgres.NewCartRepo(db)
	ctx := context.Background()

	c := mustNewCart(t, "EUR")
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status() != cart.CartStatusCheckedOut {
		t.Errorf("Status = %q, want checked_out", got.Status())
	}
}
