package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewPriceSnapshot(t *testing.T, variantID string, amount int64) pricing.PriceSnapshot {
	t.Helper()
	m := shared.MustNewMoney(amount, "USD")
	s, err := pricing.NewPriceSnapshot(id.New(), variantID, "", m)
	if err != nil {
		t.Fatalf("NewPriceSnapshot: %v", err)
	}
	return s
}

func TestPriceHistoryRepo_NilDB(t *testing.T) {
	_, err := postgres.NewPriceHistoryRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestPriceHistoryRepo_RecordAndLowestSince(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM price_history")
	t.Cleanup(func() { db.Exec("DELETE FROM price_history") })

	repo, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		t.Fatalf("NewPriceHistoryRepo: %v", err)
	}
	ctx := context.Background()

	vid := id.New()
	s := mustNewPriceSnapshot(t, vid, 1999)
	if err := repo.Record(ctx, &s); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := repo.LowestSince(ctx, vid, "USD", "", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("LowestSince: %v", err)
	}
	if got == nil {
		t.Fatal("LowestSince returned nil")
	}
	if got.Amount.Amount() != 1999 {
		t.Errorf("Amount: got %d, want 1999", got.Amount.Amount())
	}
}

func TestPriceHistoryRepo_LowestSince_ReturnsMin(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM price_history")
	t.Cleanup(func() { db.Exec("DELETE FROM price_history") })

	repo, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		t.Fatalf("NewPriceHistoryRepo: %v", err)
	}
	ctx := context.Background()

	vid := id.New()
	for _, amt := range []int64{3000, 1500, 2500} {
		s := mustNewPriceSnapshot(t, vid, amt)
		if err := repo.Record(ctx, &s); err != nil {
			t.Fatalf("Record %d: %v", amt, err)
		}
	}

	got, err := repo.LowestSince(ctx, vid, "USD", "", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("LowestSince: %v", err)
	}
	if got == nil {
		t.Fatal("LowestSince returned nil")
	}
	if got.Amount.Amount() != 1500 {
		t.Errorf("Amount: got %d, want 1500 (lowest)", got.Amount.Amount())
	}
}

func TestPriceHistoryRepo_LowestSince_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	db.Exec("DELETE FROM price_history")
	t.Cleanup(func() { db.Exec("DELETE FROM price_history") })

	repo, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		t.Fatalf("NewPriceHistoryRepo: %v", err)
	}

	got, err := repo.LowestSince(context.Background(), id.New(), "USD", "", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("LowestSince: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for no snapshots")
	}
}

func TestPriceHistoryRepo_Record_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPriceHistoryRepo(db)
	if err != nil {
		t.Fatalf("NewPriceHistoryRepo: %v", err)
	}

	if err := repo.Record(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil snapshot")
	}
}
