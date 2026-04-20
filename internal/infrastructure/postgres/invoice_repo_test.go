package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewInvoice(t *testing.T, orderID string) invoice.Invoice {
	t.Helper()
	price := shared.MustNewMoney(1000, "USD")
	tax := shared.MustNewMoney(100, "USD")
	item, err := invoice.NewItem(id.New(), "SKU-1", "Widget", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	inv, err := invoice.NewInvoice(id.New(), orderID, id.New(), "USD", []invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	return inv
}

func TestInvoiceRepo_NilDB(t *testing.T) {
	_, err := postgres.NewInvoiceRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestInvoiceRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM invoice_items")
		db.Exec("DELETE FROM invoices")
	})

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	inv := mustNewInvoice(t, id.New())
	if err := repo.Save(ctx, &inv); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if inv.InvoiceNumber() == 0 {
		t.Fatal("expected invoice number to be assigned by DB")
	}

	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID() != inv.ID() {
		t.Errorf("ID: got %q, want %q", got.ID(), inv.ID())
	}
	if got.OrderID() != inv.OrderID() {
		t.Errorf("OrderID: got %q, want %q", got.OrderID(), inv.OrderID())
	}
	if got.CustomerID() != inv.CustomerID() {
		t.Errorf("CustomerID: got %q, want %q", got.CustomerID(), inv.CustomerID())
	}
	if got.Currency() != "USD" {
		t.Errorf("Currency: got %q, want %q", got.Currency(), "USD")
	}
	if got.InvoiceNumber() != inv.InvoiceNumber() {
		t.Errorf("InvoiceNumber: got %d, want %d", got.InvoiceNumber(), inv.InvoiceNumber())
	}
	if len(got.Items()) != 1 {
		t.Fatalf("Items: got %d, want 1", len(got.Items()))
	}
	if got.Items()[0].SKU != "SKU-1" {
		t.Errorf("Item SKU: got %q, want %q", got.Items()[0].SKU, "SKU-1")
	}
}

func TestInvoiceRepo_FindByOrderID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM invoice_items")
		db.Exec("DELETE FROM invoices")
	})

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	orderID := id.New()
	inv := mustNewInvoice(t, orderID)
	if err := repo.Save(ctx, &inv); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByOrderID(ctx, orderID)
	if err != nil {
		t.Fatalf("FindByOrderID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByOrderID returned nil")
	}
	if got.ID() != inv.ID() {
		t.Errorf("ID: got %q, want %q", got.ID(), inv.ID())
	}
	if len(got.Items()) != 1 {
		t.Fatalf("Items: got %d, want 1", len(got.Items()))
	}
}

func TestInvoiceRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent invoice")
	}
}

func TestInvoiceRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	_, err = repo.FindByID(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestInvoiceRepo_FindByOrderID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	_, err = repo.FindByOrderID(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestInvoiceRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil invoice")
	}
}

func TestInvoiceRepo_MultipleItems(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM invoice_items")
		db.Exec("DELETE FROM invoices")
	})

	repo, err := postgres.NewInvoiceRepo(db)
	if err != nil {
		t.Fatalf("NewInvoiceRepo: %v", err)
	}
	ctx := context.Background()

	price1 := shared.MustNewMoney(500, "USD")
	price2 := shared.MustNewMoney(2000, "USD")
	tax := shared.MustNewMoney(250, "USD")
	item1, err := invoice.NewItem(id.New(), "SKU-A", "Item A", 2, price1)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	item2, err := invoice.NewItem(id.New(), "SKU-B", "Item B", 1, price2)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	inv, err := invoice.NewInvoice(id.New(), id.New(), id.New(), "USD",
		[]invoice.Item{item1, item2}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}

	if err := repo.Save(ctx, &inv); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if len(got.Items()) != 2 {
		t.Fatalf("Items: got %d, want 2", len(got.Items()))
	}
}
