package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewOrder(t *testing.T, customerID, currency string) order.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, currency)
	item, err := order.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, currency, []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func TestOrderRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo := postgres.NewOrderRepo(db)
	ctx := context.Background()

	o := mustNewOrder(t, "cust-1", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != o.ID {
		t.Errorf("ID = %q, want %q", got.ID, o.ID)
	}
	if got.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", got.CustomerID)
	}
	if got.Status() != order.OrderStatusPending {
		t.Errorf("Status = %q, want pending", got.Status())
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
	if got.TotalAmount.Amount() != 2000 {
		t.Errorf("TotalAmount = %d, want 2000", got.TotalAmount.Amount())
	}
	gotItems := got.Items()
	if len(gotItems) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(gotItems))
	}
	item := gotItems[0]
	if item.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want var-1", item.VariantID)
	}
	if item.SKU != "SKU-001" {
		t.Errorf("SKU = %q, want SKU-001", item.SKU)
	}
	if item.Name != "Test Product" {
		t.Errorf("Name = %q, want Test Product", item.Name)
	}
	if item.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", item.Quantity)
	}
	if item.UnitPrice.Amount() != 1000 {
		t.Errorf("UnitPrice = %d, want 1000", item.UnitPrice.Amount())
	}
}

func TestOrderRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewOrderRepo(db)
	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent order")
	}
}

func TestOrderRepo_FindByCustomerID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo := postgres.NewOrderRepo(db)
	ctx := context.Background()

	custID := "cust-" + id.New()[:8]

	o1 := mustNewOrder(t, custID, "EUR")
	if err := repo.Save(ctx, &o1); err != nil {
		t.Fatalf("Save o1: %v", err)
	}
	o2 := mustNewOrder(t, custID, "EUR")
	if err := repo.Save(ctx, &o2); err != nil {
		t.Fatalf("Save o2: %v", err)
	}

	// Different customer — should not appear.
	o3 := mustNewOrder(t, "other-cust", "EUR")
	if err := repo.Save(ctx, &o3); err != nil {
		t.Fatalf("Save o3: %v", err)
	}

	orders, err := repo.FindByCustomerID(ctx, custID)
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2", len(orders))
	}
}

func TestOrderRepo_FindByCustomerID_Empty(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewOrderRepo(db)
	orders, err := repo.FindByCustomerID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("len(orders) = %d, want 0", len(orders))
	}
}

func TestOrderRepo_UpdateStatus(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo := postgres.NewOrderRepo(db)
	ctx := context.Background()

	o := mustNewOrder(t, "cust-1", "EUR")
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := repo.UpdateStatus(ctx, &o); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status() != order.OrderStatusConfirmed {
		t.Errorf("Status = %q, want confirmed", got.Status())
	}
}

func TestOrderRepo_UpdateStatus_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo := postgres.NewOrderRepo(db)
	o := mustNewOrder(t, "cust-1", "EUR")
	// Never saved — should fail.
	if err := repo.UpdateStatus(context.Background(), &o); err == nil {
		t.Fatal("expected error for non-existent order")
	}
}

func TestOrderRepo_MultipleItems(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM order_items")
		db.Exec("DELETE FROM orders")
	})

	repo := postgres.NewOrderRepo(db)
	ctx := context.Background()

	p1 := shared.MustNewMoney(1000, "EUR")
	p2 := shared.MustNewMoney(500, "EUR")
	i1, err := order.NewItem("var-1", "SKU-1", "Shirt", 2, p1)
	if err != nil {
		t.Fatalf("NewItem i1: %v", err)
	}
	i2, err := order.NewItem("var-2", "SKU-2", "Hat", 1, p2)
	if err != nil {
		t.Fatalf("NewItem i2: %v", err)
	}

	o, err := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{i1, i2})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := repo.Save(ctx, &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Items()) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(got.Items()))
	}
	// 1000*2 + 500*1 = 2500
	if got.TotalAmount.Amount() != 2500 {
		t.Errorf("TotalAmount = %d, want 2500", got.TotalAmount.Amount())
	}
}
