package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestEmailTemplateRepo_CRUD(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM email_templates")
	mustExec(t, db, "DELETE FROM stores")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM email_templates")
		mustExec(t, db, "DELETE FROM stores")
	})

	storeRepo, err := postgres.NewStoreRepo(db)
	if err != nil {
		t.Fatalf("NewStoreRepo: %v", err)
	}
	repo, err := postgres.NewEmailTemplateRepo(db)
	if err != nil {
		t.Fatalf("NewEmailTemplateRepo: %v", err)
	}
	ctx := context.Background()

	s := mustNewStore(t, "email-tmpl-main", "Email Templates Main")
	if err := storeRepo.Create(ctx, &s); err != nil {
		t.Fatalf("Create store: %v", err)
	}

	got, err := repo.Get(ctx, s.ID, "order_confirmed", "")
	if err != nil || got != nil {
		t.Fatalf("Get empty: got=%v err=%v", got, err)
	}

	row := &mail.EmailTemplate{
		ID:        id.New(),
		StoreID:   s.ID,
		Name:      "order_confirmed",
		Locale:    "",
		Subject:   "Thanks {{.Data.OrderID}}",
		Body:      "<p>Order {{.Data.OrderID}}</p>",
		UpdatedBy: "admin-1",
	}
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err = repo.Get(ctx, s.ID, "order_confirmed", "")
	if err != nil || got == nil {
		t.Fatalf("Get: got=%v err=%v", got, err)
	}
	if got.Subject != row.Subject || got.Body != row.Body || got.ID == "" {
		t.Fatalf("mismatch: %#v", got)
	}

	row.Subject = "Updated subject"
	row.Body = "<p>updated</p>"
	row.UpdatedBy = "admin-1"
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	got, err = repo.Get(ctx, s.ID, "order_confirmed", "")
	if err != nil || got == nil {
		t.Fatalf("Get after update: %v %v", got, err)
	}
	if got.Subject != "Updated subject" || got.UpdatedBy != "admin-1" {
		t.Fatalf("update mismatch: %#v", got)
	}

	de := &mail.EmailTemplate{
		StoreID:   s.ID,
		Name:      "order_confirmed",
		Locale:    "DE",
		Subject:   "Danke",
		Body:      "<p>de</p>",
		UpdatedBy: "admin-2",
	}
	if err := repo.Upsert(ctx, de); err != nil {
		t.Fatalf("Upsert de: %v", err)
	}
	list, err := repo.ListByStore(ctx, s.ID)
	if err != nil {
		t.Fatalf("ListByStore: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}

	// Canonical locale: DE upsert is stored/fetched as de.
	gotDE, err := repo.Get(ctx, s.ID, "order_confirmed", "de")
	if err != nil || gotDE == nil || gotDE.Locale != "de" {
		t.Fatalf("Get de: got=%v err=%v", gotDE, err)
	}
	firstUpdated := got.UpdatedAt
	row.Subject = "Updated again"
	row.UpdatedBy = "admin-3"
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	got, err = repo.Get(ctx, s.ID, "order_confirmed", "")
	if err != nil || got == nil {
		t.Fatalf("Get after second update: %v %v", got, err)
	}
	if !got.UpdatedAt.After(firstUpdated) {
		t.Fatalf("updated_at did not advance: first=%v second=%v", firstUpdated, got.UpdatedAt)
	}

	if err := repo.Delete(ctx, s.ID, "order_confirmed", "de"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = repo.Get(ctx, s.ID, "order_confirmed", "de")
	if err != nil || got != nil {
		t.Fatalf("Get after delete: got=%v err=%v", got, err)
	}
	if err := repo.Delete(ctx, s.ID, "order_confirmed", "de"); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

func TestEmailTemplateRepo_UnknownStore(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewEmailTemplateRepo(db)
	if err != nil {
		t.Fatalf("NewEmailTemplateRepo: %v", err)
	}
	err = repo.Upsert(context.Background(), &mail.EmailTemplate{
		StoreID:   "missing-store",
		Name:      "order_confirmed",
		Subject:   "x",
		Body:      "<p>x</p>",
		UpdatedBy: "admin-1",
	})
	if err == nil {
		t.Fatal("expected store not found")
	}
}

func TestEmailTemplateRepo_NilDB(t *testing.T) {
	if _, err := postgres.NewEmailTemplateRepo(nil); err == nil {
		t.Fatal("expected error")
	}
}
