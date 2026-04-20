package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewCoupon(t *testing.T, code, promoID string) promotion.Coupon {
	t.Helper()
	c, err := promotion.NewCoupon(id.New(), code, promoID)
	if err != nil {
		t.Fatalf("NewCoupon: %v", err)
	}
	return c
}

func TestCouponRepo_NilDB(t *testing.T) {
	_, err := postgres.NewCouponRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestCouponRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM coupons") })

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	promoID := id.New()
	c := mustNewCoupon(t, "SAVE10", promoID)
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
	if got.Code != "SAVE10" {
		t.Errorf("Code: got %q, want %q", got.Code, "SAVE10")
	}
	if got.PromotionID != promoID {
		t.Errorf("PromotionID: got %q, want %q", got.PromotionID, promoID)
	}
	if !got.Active {
		t.Error("expected coupon to be active")
	}
}

func TestCouponRepo_FindByCode(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM coupons") })

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCoupon(t, "DISCOUNT20", id.New())
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByCode(ctx, "DISCOUNT20")
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got == nil {
		t.Fatal("FindByCode returned nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID: got %q, want %q", got.ID, c.ID)
	}
}

func TestCouponRepo_FindByCode_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindByCode(ctx, "NOPE")
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent coupon")
	}
}

func TestCouponRepo_ListByPromotion(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM coupons") })

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	promoID := id.New()
	c1 := mustNewCoupon(t, "LISTA", promoID)
	c2 := mustNewCoupon(t, "LISTB", promoID)
	otherC := mustNewCoupon(t, "OTHER", id.New())
	for _, c := range []*promotion.Coupon{&c1, &c2, &otherC} {
		if err := repo.Save(ctx, c); err != nil {
			t.Fatalf("Save %q: %v", c.Code, err)
		}
	}

	coupons, err := repo.ListByPromotion(ctx, promoID)
	if err != nil {
		t.Fatalf("ListByPromotion: %v", err)
	}
	if len(coupons) != 2 {
		t.Fatalf("ListByPromotion: got %d, want 2", len(coupons))
	}
}

func TestCouponRepo_SaveUpsert(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM coupons") })

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCoupon(t, "UPSERT", id.New())
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Update and save again — upsert should succeed.
	c.Active = false
	c.UsageLimit = 5
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Active {
		t.Error("expected coupon to be inactive after upsert")
	}
	if got.UsageLimit != 5 {
		t.Errorf("UsageLimit: got %d, want 5", got.UsageLimit)
	}
}

func TestCouponRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM coupons") })

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCoupon(t, "DELME", id.New())
	if err := repo.Save(ctx, &c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := repo.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestCouponRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	err = repo.Delete(ctx, id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent coupon")
	}
}

func TestCouponRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCouponRepo(db)
	if err != nil {
		t.Fatalf("NewCouponRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil coupon")
	}
}
