package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func distinctOrderedUUIDs(t *testing.T) (low, high string) {
	t.Helper()
	a, b := id.New(), id.New()
	if a == b {
		t.Fatal("uuid collision")
	}
	if a < b {
		return a, b
	}
	return b, a
}

func TestBundleComponentRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM bundle_components")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM bundle_components") })

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()
	bundle := mustNewProduct(t, "Tx Bundle", "tx-bundle")
	bundle.Type = catalog.TypeBundle
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	component := mustNewProduct(t, "Tx Mug", "tx-mug")
	if err := productRepo.Create(ctx, &component); err != nil {
		t.Fatalf("create component: %v", err)
	}
	link, err := catalog.NewBundleComponent(bundle.ID, component.ID, 2)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Upsert(ctx, link); err != nil {
		t.Fatalf("Upsert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	list, err := repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("rolled-back upsert visible: %+v", list)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Upsert(ctx, link); err != nil {
		t.Fatalf("Upsert in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	list, err = repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID after commit: %v", err)
	}
	if len(list) != 1 || list[0].Quantity != 2 {
		t.Fatalf("committed list = %+v", list)
	}
}

func TestBundleComponentRepo_NilDB(t *testing.T) {
	_, err := postgres.NewBundleComponentRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestBundleComponentRepo_UpsertListRemove(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM bundle_components")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM bundle_components")
	})

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	bundle := mustNewProduct(t, "Gift Box", "gift-box")
	bundle.Type = catalog.TypeBundle
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	component := mustNewProduct(t, "Mug", "mug")
	if err := productRepo.Create(ctx, &component); err != nil {
		t.Fatalf("create component: %v", err)
	}

	link, err := catalog.NewBundleComponent(bundle.ID, component.ID, 2)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	if err := repo.Upsert(ctx, link); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	list, err := repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if list[0].Quantity != 2 || !list[0].Required {
		t.Errorf("list[0] = %+v", list[0])
	}

	n, err := repo.CountByBundleID(ctx, bundle.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByBundleID = %d, %v, want 1", n, err)
	}
	n, err = repo.CountByComponentID(ctx, component.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByComponentID = %d, %v, want 1", n, err)
	}

	link.Quantity = 5
	link.Required = false
	if err := repo.Upsert(ctx, link); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	list, err = repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID after update: %v", err)
	}
	if list[0].Quantity != 5 || list[0].Required {
		t.Errorf("after update list[0] = %+v", list[0])
	}

	if err := repo.Remove(ctx, bundle.ID, component.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, err = repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID after remove: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %+v", list)
	}

	err = repo.Remove(ctx, bundle.ID, component.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Remove missing = %v, want not found", err)
	}
}

func TestBundleComponentRepo_ListEmptyNonNil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	bundle := mustNewProduct(t, "Empty Bundle", "empty-bundle")
	bundle.Type = catalog.TypeBundle
	productRepo, _ := postgres.NewProductRepo(db)
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	list, err := repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil empty slice")
	}
}

func TestBundleComponentRepo_ListEmptyID(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	_, err = repo.ListByBundleID(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ListByBundleID = %v, want empty id error", err)
	}
}

func TestBundleComponentRepo_UpsertUnknownProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	link, err := catalog.NewBundleComponent(id.New(), id.New(), 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	err = repo.Upsert(ctx, link)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Upsert = %v, want not found", err)
	}
}

func TestBundleComponentRepo_UpsertInvalidQuantity(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	productRepo, _ := postgres.NewProductRepo(db)
	bundle := mustNewProduct(t, "B", "bundle-qty")
	bundle.Type = catalog.TypeBundle
	component := mustNewProduct(t, "C", "component-qty")
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if err := productRepo.Create(ctx, &component); err != nil {
		t.Fatalf("create component: %v", err)
	}

	link := &catalog.BundleComponent{
		BundleProductID:    bundle.ID,
		ComponentProductID: component.ID,
		Quantity:           10000,
		Required:           true,
	}
	err = repo.Upsert(ctx, link)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Upsert = %v, want validation", err)
	}
}

func TestBundleComponentRepo_ComponentDeleteRestricted(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM bundle_components")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM bundle_components")
	})

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	bundle := mustNewProduct(t, "Kit", "kit-restrict")
	bundle.Type = catalog.TypeBundle
	component := mustNewProduct(t, "Part", "part-restrict")
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if err := productRepo.Create(ctx, &component); err != nil {
		t.Fatalf("create component: %v", err)
	}
	link, err := catalog.NewBundleComponent(bundle.ID, component.ID, 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	if err := repo.Upsert(ctx, link); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	_, err = db.Exec("DELETE FROM products WHERE id = $1", component.ID)
	if err == nil {
		t.Fatal("expected FK RESTRICT when deleting component still linked to bundle")
	}
}

func TestBundleComponentRepo_ListOrderingSameCreatedAt(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM bundle_components")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM bundle_components")
	})

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	bundle := mustNewProduct(t, "Starter Kit", "starter-kit-order")
	bundle.Type = catalog.TypeBundle
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	idLow, idHigh := distinctOrderedUUIDs(t)
	for i, spec := range []struct {
		id, name string
	}{
		{idHigh, "Beta"},
		{idLow, "Alpha"},
	} {
		p := mustNewProduct(t, spec.name, spec.name+"-order")
		p.ID = spec.id
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("create component %d: %v", i, err)
		}
		link, err := catalog.NewBundleComponent(bundle.ID, p.ID, 1)
		if err != nil {
			t.Fatalf("NewBundleComponent: %v", err)
		}
		if err := repo.Upsert(ctx, link); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	ts := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	mustExec(t, db, `UPDATE bundle_components SET created_at = $1 WHERE bundle_product_id = $2`, ts, bundle.ID)

	list, err := repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].ComponentProductID != idLow || list[1].ComponentProductID != idHigh {
		t.Errorf("expected ascending component_product_id tie-break, got %s then %s",
			list[0].ComponentProductID, list[1].ComponentProductID)
	}
}

func TestBundleComponentRepo_ListOrderingByCreatedAt(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM bundle_components")
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM bundle_components")
	})

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	bundle := mustNewProduct(t, "Chrono Kit", "chrono-kit")
	bundle.Type = catalog.TypeBundle
	if err := productRepo.Create(ctx, &bundle); err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	idEarly, idLate := distinctOrderedUUIDs(t)
	// idLate is the greater component_product_id. Give it the earlier created_at
	// so list order follows the timestamp rather than the id.
	for _, spec := range []struct {
		id, name string
	}{
		{idLate, "LateID"},
		{idEarly, "EarlyID"},
	} {
		p := mustNewProduct(t, spec.name, spec.name+"-chrono")
		p.ID = spec.id
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("create component: %v", err)
		}
		link, err := catalog.NewBundleComponent(bundle.ID, p.ID, 1)
		if err != nil {
			t.Fatalf("NewBundleComponent: %v", err)
		}
		if err := repo.Upsert(ctx, link); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	earlyAt := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	lateAt := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	mustExec(t, db, `UPDATE bundle_components SET created_at = $1 WHERE component_product_id = $2`, earlyAt, idLate)
	mustExec(t, db, `UPDATE bundle_components SET created_at = $1 WHERE component_product_id = $2`, lateAt, idEarly)

	list, err := repo.ListByBundleID(ctx, bundle.ID)
	if err != nil {
		t.Fatalf("ListByBundleID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].ComponentProductID != idLate || list[1].ComponentProductID != idEarly {
		t.Errorf("expected created_at order (later id first), got %s then %s",
			list[0].ComponentProductID, list[1].ComponentProductID)
	}
}
