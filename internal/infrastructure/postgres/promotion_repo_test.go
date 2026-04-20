package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewPromotion(t *testing.T, name string) promotion.Promotion {
	t.Helper()
	p, err := promotion.NewPromotion(id.New(), name, promotion.TypeCatalog)
	if err != nil {
		t.Fatalf("NewPromotion: %v", err)
	}
	return p
}

func TestPromotionRepo_NilDB(t *testing.T) {
	_, err := postgres.NewPromotionRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestPromotionRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM promotions") })

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPromotion(t, "Summer Sale")
	if err := repo.Save(ctx, &p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.Name != "Summer Sale" {
		t.Errorf("Name: got %q, want %q", got.Name, "Summer Sale")
	}
	if got.Type != promotion.TypeCatalog {
		t.Errorf("Type: got %q, want %q", got.Type, promotion.TypeCatalog)
	}
	if !got.Active {
		t.Error("expected promotion to be active")
	}
}

func TestPromotionRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent promotion")
	}
}

func TestPromotionRepo_ListActive(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM promotions") })

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	// Create two catalog promos and one cart promo.
	cat1 := mustNewPromotion(t, "Cat1")
	cat1.Priority = 2
	cat2 := mustNewPromotion(t, "Cat2")
	cat2.Priority = 1

	cart, err := promotion.NewPromotion(id.New(), "CartPromo", promotion.TypeCart)
	if err != nil {
		t.Fatalf("NewPromotion cart: %v", err)
	}

	for _, p := range []*promotion.Promotion{&cat1, &cat2, &cart} {
		if err := repo.Save(ctx, p); err != nil {
			t.Fatalf("Save %q: %v", p.Name, err)
		}
	}

	// List catalog: should only return catalog promos, ordered by priority ASC.
	result, err := repo.ListActive(ctx, promotion.TypeCatalog)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListActive catalog: got %d, want 2", len(result))
	}
	if result[0].Priority > result[1].Priority {
		t.Errorf("expected ascending priority: got %d, %d", result[0].Priority, result[1].Priority)
	}

	// List cart: should only return the cart promo.
	cartResult, err := repo.ListActive(ctx, promotion.TypeCart)
	if err != nil {
		t.Fatalf("ListActive cart: %v", err)
	}
	if len(cartResult) != 1 {
		t.Fatalf("ListActive cart: got %d, want 1", len(cartResult))
	}
}

func TestPromotionRepo_ListActive_InactiveExcluded(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM promotions") })

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPromotion(t, "Inactive")
	p.Active = false
	if err := repo.Save(ctx, &p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result, err := repo.ListActive(ctx, promotion.TypeCatalog)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("ListActive: got %d, want 0 (inactive excluded)", len(result))
	}
}

func TestPromotionRepo_SaveUpsert(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM promotions") })

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPromotion(t, "Original")
	if err := repo.Save(ctx, &p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p.Name = "Updated"
	p.Priority = 99
	if err := repo.Save(ctx, &p); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("Name: got %q, want %q", got.Name, "Updated")
	}
	if got.Priority != 99 {
		t.Errorf("Priority: got %d, want 99", got.Priority)
	}
}

func TestPromotionRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM promotions") })

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPromotion(t, "ToDelete")
	if err := repo.Save(ctx, &p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestPromotionRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	err = repo.Delete(ctx, id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent promotion")
	}
}

func TestPromotionRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPromotionRepo(db)
	if err != nil {
		t.Fatalf("NewPromotionRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil promotion")
	}
}
