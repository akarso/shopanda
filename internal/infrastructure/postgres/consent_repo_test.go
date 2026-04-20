package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewConsent(t *testing.T, customerID string) *legal.Consent {
	t.Helper()
	c, err := legal.NewConsent(customerID)
	if err != nil {
		t.Fatalf("NewConsent: %v", err)
	}
	return &c
}

func TestConsentRepo_NilDB(t *testing.T) {
	_, err := postgres.NewConsentRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestConsentRepo_UpsertAndFind(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM consents")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM consents") })

	repo, err := postgres.NewConsentRepo(db)
	if err != nil {
		t.Fatalf("NewConsentRepo: %v", err)
	}
	ctx := context.Background()

	custID := id.New()
	c := mustNewConsent(t, custID)
	c.Analytics = true
	c.Marketing = false

	if err := repo.Upsert(ctx, c); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.FindByCustomerID(ctx, custID)
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByCustomerID returned nil")
	}
	if got.CustomerID != custID {
		t.Errorf("CustomerID: got %q, want %q", got.CustomerID, custID)
	}
	if !got.Necessary {
		t.Error("expected Necessary=true")
	}
	if !got.Analytics {
		t.Error("expected Analytics=true")
	}
	if got.Marketing {
		t.Error("expected Marketing=false")
	}
}

func TestConsentRepo_Upsert_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM consents")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM consents") })

	repo, err := postgres.NewConsentRepo(db)
	if err != nil {
		t.Fatalf("NewConsentRepo: %v", err)
	}
	ctx := context.Background()

	custID := id.New()
	c := mustNewConsent(t, custID)
	c.Marketing = false
	if err := repo.Upsert(ctx, c); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	c.Marketing = true
	if err := repo.Upsert(ctx, c); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	got, err := repo.FindByCustomerID(ctx, custID)
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByCustomerID returned nil after upsert update")
	}
	if !got.Marketing {
		t.Error("expected Marketing=true after upsert update")
	}
}

func TestConsentRepo_FindByCustomerID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewConsentRepo(db)
	if err != nil {
		t.Fatalf("NewConsentRepo: %v", err)
	}

	got, err := repo.FindByCustomerID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent customer")
	}
}

func TestConsentRepo_DeleteByCustomerID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM consents")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM consents") })

	repo, err := postgres.NewConsentRepo(db)
	if err != nil {
		t.Fatalf("NewConsentRepo: %v", err)
	}
	ctx := context.Background()

	custID := id.New()
	c := mustNewConsent(t, custID)
	if err := repo.Upsert(ctx, c); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.DeleteByCustomerID(ctx, custID); err != nil {
		t.Fatalf("DeleteByCustomerID: %v", err)
	}

	got, err := repo.FindByCustomerID(ctx, custID)
	if err != nil {
		t.Fatalf("FindByCustomerID: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}
