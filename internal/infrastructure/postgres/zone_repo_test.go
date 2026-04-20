package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewZone(t *testing.T, name string) shipping.Zone {
	t.Helper()
	z, err := shipping.NewZone(id.New(), name, []string{"US"}, 1)
	if err != nil {
		t.Fatalf("NewZone: %v", err)
	}
	return z
}

func mustNewRateTier(t *testing.T, zoneID string) shipping.RateTier {
	t.Helper()
	price := shared.MustNewMoney(500, "USD")
	rt, err := shipping.NewRateTier(id.New(), zoneID, 0, 5.0, price)
	if err != nil {
		t.Fatalf("NewRateTier: %v", err)
	}
	return rt
}

func TestZoneRepo_NilDB(t *testing.T) {
	_, err := postgres.NewZoneRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestZoneRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "Domestic")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	got, err := repo.FindZoneByID(ctx, z.ID)
	if err != nil {
		t.Fatalf("FindZoneByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindZoneByID returned nil")
	}
	if got.Name != "Domestic" {
		t.Errorf("Name: got %q, want %q", got.Name, "Domestic")
	}
	if len(got.Countries) != 1 || got.Countries[0] != "US" {
		t.Errorf("Countries: got %v, want [US]", got.Countries)
	}
	if !got.Active {
		t.Error("expected zone to be active")
	}
}

func TestZoneRepo_FindZoneByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	got, err := repo.FindZoneByID(ctx, id.New())
	if err != nil {
		t.Fatalf("FindZoneByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent zone")
	}
}

func TestZoneRepo_FindZoneByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	_, err = repo.FindZoneByID(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestZoneRepo_ListZones(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z1 := mustNewZone(t, "Zone A")
	z1.Priority = 1
	z2 := mustNewZone(t, "Zone B")
	z2.Priority = 2
	for _, z := range []*shipping.Zone{&z1, &z2} {
		if err := repo.CreateZone(ctx, z); err != nil {
			t.Fatalf("CreateZone %q: %v", z.Name, err)
		}
	}

	zones, err := repo.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) < 2 {
		t.Fatalf("ListZones: got %d, want >= 2", len(zones))
	}
}

func TestZoneRepo_UpdateZone(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "BeforeUpdate")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	z.Name = "AfterUpdate"
	z.Countries = []string{"US", "CA"}
	if err := repo.UpdateZone(ctx, &z); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}

	got, err := repo.FindZoneByID(ctx, z.ID)
	if err != nil {
		t.Fatalf("FindZoneByID: %v", err)
	}
	if got.Name != "AfterUpdate" {
		t.Errorf("Name: got %q, want %q", got.Name, "AfterUpdate")
	}
	if len(got.Countries) != 2 {
		t.Errorf("Countries: got %v, want [US CA]", got.Countries)
	}
}

func TestZoneRepo_UpdateZone_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "Ghost")
	err = repo.UpdateZone(ctx, &z)
	if err == nil {
		t.Fatal("expected error updating non-existent zone")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound error, got: %v", err)
	}
}

func TestZoneRepo_DeleteZone(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "ToDelete")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	if err := repo.DeleteZone(ctx, z.ID); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}

	got, err := repo.FindZoneByID(ctx, z.ID)
	if err != nil {
		t.Fatalf("FindZoneByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestZoneRepo_DeleteZone_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	err = repo.DeleteZone(ctx, id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent zone")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound error, got: %v", err)
	}
}

func TestZoneRepo_CreateZone_Duplicate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "Dup")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	// Same ID → conflict.
	err = repo.CreateZone(ctx, &z)
	if err == nil {
		t.Fatal("expected conflict error for duplicate zone")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict error, got: %v", err)
	}
}

// ── Rate Tier operations ────────────────────────────────────────────────

func TestZoneRepo_CreateRateTierAndFind(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "RateTierZone")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	rt := mustNewRateTier(t, z.ID)
	if err := repo.CreateRateTier(ctx, &rt); err != nil {
		t.Fatalf("CreateRateTier: %v", err)
	}

	got, err := repo.FindRateTierByID(ctx, rt.ID)
	if err != nil {
		t.Fatalf("FindRateTierByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindRateTierByID returned nil")
	}
	if got.ZoneID != z.ID {
		t.Errorf("ZoneID: got %q, want %q", got.ZoneID, z.ID)
	}
	if got.MaxWeight != 5.0 {
		t.Errorf("MaxWeight: got %v, want 5", got.MaxWeight)
	}
}

func TestZoneRepo_ListRateTiers(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "ListTiersZone")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	rt1 := mustNewRateTier(t, z.ID)
	rt1.MinWeight = 0
	rt1.MaxWeight = 5.0
	rt2 := mustNewRateTier(t, z.ID)
	rt2.MinWeight = 5.0
	rt2.MaxWeight = 10.0
	for _, rt := range []*shipping.RateTier{&rt1, &rt2} {
		if err := repo.CreateRateTier(ctx, rt); err != nil {
			t.Fatalf("CreateRateTier: %v", err)
		}
	}

	tiers, err := repo.ListRateTiers(ctx, z.ID)
	if err != nil {
		t.Fatalf("ListRateTiers: %v", err)
	}
	if len(tiers) != 2 {
		t.Fatalf("ListRateTiers: got %d, want 2", len(tiers))
	}
	// Should be ordered by min_weight ASC.
	if tiers[0].MinWeight > tiers[1].MinWeight {
		t.Errorf("expected ascending min_weight: got %v, %v", tiers[0].MinWeight, tiers[1].MinWeight)
	}
}

func TestZoneRepo_UpdateRateTier(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "UpdTierZone")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	rt := mustNewRateTier(t, z.ID)
	if err := repo.CreateRateTier(ctx, &rt); err != nil {
		t.Fatalf("CreateRateTier: %v", err)
	}

	rt.MaxWeight = 20.0
	rt.Price = shared.MustNewMoney(1500, "USD")
	if err := repo.UpdateRateTier(ctx, &rt); err != nil {
		t.Fatalf("UpdateRateTier: %v", err)
	}

	got, err := repo.FindRateTierByID(ctx, rt.ID)
	if err != nil {
		t.Fatalf("FindRateTierByID: %v", err)
	}
	if got.MaxWeight != 20.0 {
		t.Errorf("MaxWeight: got %v, want 20", got.MaxWeight)
	}
	if got.Price.Amount() != 1500 {
		t.Errorf("Price: got %d, want 1500", got.Price.Amount())
	}
}

func TestZoneRepo_DeleteRateTier(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	t.Cleanup(func() {
		db.Exec("DELETE FROM shipping_rate_tiers")
		db.Exec("DELETE FROM shipping_zones")
	})

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	z := mustNewZone(t, "DelTierZone")
	if err := repo.CreateZone(ctx, &z); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	rt := mustNewRateTier(t, z.ID)
	if err := repo.CreateRateTier(ctx, &rt); err != nil {
		t.Fatalf("CreateRateTier: %v", err)
	}

	if err := repo.DeleteRateTier(ctx, rt.ID); err != nil {
		t.Fatalf("DeleteRateTier: %v", err)
	}

	got, err := repo.FindRateTierByID(ctx, rt.ID)
	if err != nil {
		t.Fatalf("FindRateTierByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestZoneRepo_DeleteRateTier_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	err = repo.DeleteRateTier(ctx, id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent rate tier")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound error, got: %v", err)
	}
}

func TestZoneRepo_CreateRateTier_InvalidZone(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	rt := mustNewRateTier(t, id.New()) // non-existent zone
	err = repo.CreateRateTier(ctx, &rt)
	if err == nil {
		t.Fatal("expected error for invalid zone reference")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("expected Validation error, got: %v", err)
	}
}

func TestZoneRepo_CreateZone_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewZoneRepo(db)
	if err != nil {
		t.Fatalf("NewZoneRepo: %v", err)
	}
	ctx := context.Background()

	if err := repo.CreateZone(ctx, nil); err == nil {
		t.Fatal("expected error for nil zone")
	}
}
