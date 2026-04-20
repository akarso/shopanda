package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewCreditNote(t *testing.T, invoiceID string) invoice.CreditNote {
	t.Helper()
	price := shared.MustNewMoney(1000, "USD")
	tax := shared.MustNewMoney(100, "USD")
	item, err := invoice.NewItem(id.New(), "SKU-CN", "Refund Item", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	cn, err := invoice.NewCreditNote(id.New(), invoiceID, id.New(), id.New(), "damaged", "USD",
		[]invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewCreditNote: %v", err)
	}
	return cn
}

func TestCreditNoteRepo_NilDB(t *testing.T) {
	_, err := postgres.NewCreditNoteRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestCreditNoteRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM credit_note_items")
		db.Exec("DELETE FROM credit_notes")
	})

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	cn := mustNewCreditNote(t, id.New())
	if err := repo.Save(ctx, &cn); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cn.CreditNoteNumber == 0 {
		t.Fatal("expected credit note number to be assigned by DB")
	}

	got, err := repo.FindByID(ctx, cn.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != cn.ID {
		t.Errorf("ID: got %q, want %q", got.ID, cn.ID)
	}
	if got.InvoiceID != cn.InvoiceID {
		t.Errorf("InvoiceID: got %q, want %q", got.InvoiceID, cn.InvoiceID)
	}
	if got.Reason != "damaged" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "damaged")
	}
	if got.CreditNoteNumber != cn.CreditNoteNumber {
		t.Errorf("CreditNoteNumber: got %d, want %d", got.CreditNoteNumber, cn.CreditNoteNumber)
	}
	if len(got.Items()) != 1 {
		t.Fatalf("Items: got %d, want 1", len(got.Items()))
	}
	if got.Items()[0].SKU != "SKU-CN" {
		t.Errorf("Item SKU: got %q, want %q", got.Items()[0].SKU, "SKU-CN")
	}
}

func TestCreditNoteRepo_FindByInvoiceID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM credit_note_items")
		db.Exec("DELETE FROM credit_notes")
	})

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	invoiceID := id.New()
	cn1 := mustNewCreditNote(t, invoiceID)
	cn2 := mustNewCreditNote(t, invoiceID)
	if err := repo.Save(ctx, &cn1); err != nil {
		t.Fatalf("Save cn1: %v", err)
	}
	if err := repo.Save(ctx, &cn2); err != nil {
		t.Fatalf("Save cn2: %v", err)
	}

	notes, err := repo.FindByInvoiceID(ctx, invoiceID)
	if err != nil {
		t.Fatalf("FindByInvoiceID: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("FindByInvoiceID: got %d notes, want 2", len(notes))
	}
	// Should have items loaded.
	for i, n := range notes {
		if len(n.Items()) == 0 {
			t.Errorf("note[%d]: expected items", i)
		}
	}
}

func TestCreditNoteRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent credit note")
	}
}

func TestCreditNoteRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	_, err = repo.FindByID(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCreditNoteRepo_FindByInvoiceID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	_, err = repo.FindByInvoiceID(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty invoice id")
	}
}

func TestCreditNoteRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCreditNoteRepo(db)
	if err != nil {
		t.Fatalf("NewCreditNoteRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil credit note")
	}
}
