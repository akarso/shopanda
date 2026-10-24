package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestLinkedEligibilityRepo_Migration096Applied(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	for _, name := range []string{"linked_eligible_parents", "linked_eligible_categories"} {
		var rel sql.NullString
		if err := db.QueryRow(`SELECT to_regclass($1)::text`, "public."+name).Scan(&rel); err != nil {
			t.Fatalf("to_regclass %s: %v", name, err)
		}
		if !rel.Valid || rel.String != name {
			t.Fatalf("%s missing (migration 096 not applied): %v", name, rel)
		}
	}
}

func TestLinkedEligibilityRepo_NilDB(t *testing.T) {
	_, err := postgres.NewLinkedEligibilityRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestLinkedEligibilityRepo_InsertListRemoveParent(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Ticket", "ticket-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent := mustNewProduct(t, "Card", "card-"+id.New())
	if err := productRepo.Create(ctx, &parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	link, err := catalog.NewLinkedEligibleParent(child.ID, parent.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	if err := repo.InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent: %v", err)
	}
	if err := repo.InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent duplicate: %v", err)
	}

	list, err := repo.ListParentsByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentsByChildID: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 (insert-ignore)", len(list))
	}
	if list[0].ParentProductID != parent.ID {
		t.Fatalf("parent = %s, want %s", list[0].ParentProductID, parent.ID)
	}

	n, err := repo.CountRulesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("CountRulesByChildID: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountRulesByChildID = %d, want 1", n)
	}

	ok, err := repo.IsEligible(ctx, child.ID, parent.ID)
	if err != nil {
		t.Fatalf("IsEligible: %v", err)
	}
	if !ok {
		t.Fatal("expected allowlist match")
	}

	if err := repo.RemoveParent(ctx, child.ID, parent.ID); err != nil {
		t.Fatalf("RemoveParent: %v", err)
	}
	err = repo.RemoveParent(ctx, child.ID, parent.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("RemoveParent missing = %v, want not found", err)
	}
	ok, err = repo.IsEligible(ctx, child.ID, parent.ID)
	if err != nil {
		t.Fatalf("IsEligible after remove: %v", err)
	}
	if ok {
		t.Fatal("expected not eligible after remove")
	}
}

func TestLinkedEligibilityRepo_InsertListRemoveCategory(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	categoryRepo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Pass", "pass-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent := mustNewProduct(t, "Physical Card", "phys-card-"+id.New())
	if err := productRepo.Create(ctx, &parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	other := mustNewProduct(t, "Unrelated", "unrelated-"+id.New())
	if err := productRepo.Create(ctx, &other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	cat := mustNewCategory(t, "Cards", "cards-"+id.New())
	if err := categoryRepo.Create(ctx, &cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := productRepo.AssignCategory(ctx, parent.ID, cat.ID); err != nil {
		t.Fatalf("AssignCategory: %v", err)
	}

	rule, err := catalog.NewLinkedEligibleCategory(child.ID, cat.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}
	if err := repo.InsertCategory(ctx, rule); err != nil {
		t.Fatalf("InsertCategory: %v", err)
	}
	if err := repo.InsertCategory(ctx, rule); err != nil {
		t.Fatalf("InsertCategory duplicate: %v", err)
	}

	list, err := repo.ListCategoriesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListCategoriesByChildID: %v", err)
	}
	if len(list) != 1 || list[0].CategoryID != cat.ID {
		t.Fatalf("list = %+v", list)
	}
	n, err := repo.CountRulesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("CountRulesByChildID: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountRulesByChildID = %d, want 1", n)
	}

	ok, err := repo.IsEligible(ctx, child.ID, parent.ID)
	if err != nil {
		t.Fatalf("IsEligible parent in category: %v", err)
	}
	if !ok {
		t.Fatal("expected category match")
	}
	ok, err = repo.IsEligible(ctx, child.ID, other.ID)
	if err != nil {
		t.Fatalf("IsEligible unrelated: %v", err)
	}
	if ok {
		t.Fatal("unrelated product must not match the category rule")
	}

	if err := repo.RemoveCategory(ctx, child.ID, cat.ID); err != nil {
		t.Fatalf("RemoveCategory: %v", err)
	}
	err = repo.RemoveCategory(ctx, child.ID, cat.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("RemoveCategory missing = %v, want not found", err)
	}
}

func TestLinkedEligibilityRepo_IsEligible_OR(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	categoryRepo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Zone Pass", "zone-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	allowlisted := mustNewProduct(t, "VIP Card", "vip-"+id.New())
	if err := productRepo.Create(ctx, &allowlisted); err != nil {
		t.Fatalf("create allowlisted: %v", err)
	}
	inCategory := mustNewProduct(t, "City Card", "city-"+id.New())
	if err := productRepo.Create(ctx, &inCategory); err != nil {
		t.Fatalf("create in-category: %v", err)
	}
	cat := mustNewCategory(t, "Transit Cards", "transit-"+id.New())
	if err := categoryRepo.Create(ctx, &cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := productRepo.AssignCategory(ctx, inCategory.ID, cat.ID); err != nil {
		t.Fatalf("AssignCategory: %v", err)
	}

	parentLink, err := catalog.NewLinkedEligibleParent(child.ID, allowlisted.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	if err := repo.InsertParent(ctx, parentLink); err != nil {
		t.Fatalf("InsertParent: %v", err)
	}
	catLink, err := catalog.NewLinkedEligibleCategory(child.ID, cat.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}
	if err := repo.InsertCategory(ctx, catLink); err != nil {
		t.Fatalf("InsertCategory: %v", err)
	}

	n, err := repo.CountRulesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("CountRulesByChildID: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountRulesByChildID = %d, want 2", n)
	}

	for _, parentID := range []string{allowlisted.ID, inCategory.ID} {
		ok, err := repo.IsEligible(ctx, child.ID, parentID)
		if err != nil {
			t.Fatalf("IsEligible %s: %v", parentID, err)
		}
		if !ok {
			t.Fatalf("expected eligible for %s", parentID)
		}
	}
	ok, err := repo.IsEligible(ctx, child.ID, child.ID)
	if err != nil {
		t.Fatalf("IsEligible self: %v", err)
	}
	if ok {
		t.Fatal("self must not be eligible")
	}
}

func TestLinkedEligibilityRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()
	child := mustNewProduct(t, "Tx Child", "tx-child-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent := mustNewProduct(t, "Tx Parent", "tx-parent-"+id.New())
	if err := productRepo.Create(ctx, &parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	link, err := catalog.NewLinkedEligibleParent(child.ID, parent.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	list, err := repo.ListParentsByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentsByChildID: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("rolled-back insert visible: %+v", list)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	list, err = repo.ListParentsByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentsByChildID after commit: %v", err)
	}
	if len(list) != 1 || list[0].ParentProductID != parent.ID {
		t.Fatalf("committed list = %+v", list)
	}
}

func TestLinkedEligibilityRepo_InsertUnknownProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	link, err := catalog.NewLinkedEligibleParent(id.New(), id.New())
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	err = repo.InsertParent(context.Background(), link)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("InsertParent = %v, want not found (23503)", err)
	}
}

func TestLinkedEligibilityRepo_InsertInvalidUUID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}

	link := &catalog.LinkedEligibleParent{ChildProductID: "not-a-uuid", ParentProductID: "also-not"}
	err = repo.InsertParent(context.Background(), link)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("InsertParent = %v, want validation (22P02)", err)
	}
	if !strings.Contains(err.Error(), "invalid product id") {
		t.Fatalf("InsertParent = %v, want invalid product id", err)
	}
}

func TestLinkedEligibilityRepo_SelfReferenceCheck(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	ctx := productRepoAdminCtx()
	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	child := mustNewProduct(t, "Self Ticket", "self-ticket-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	err = repo.InsertParent(ctx, &catalog.LinkedEligibleParent{ChildProductID: child.ID, ParentProductID: child.ID})
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("InsertParent self = %v, want validation", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO linked_eligible_parents (child_product_id, parent_product_id) VALUES ($1, $1)`, child.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("raw self insert = %v, want SQLSTATE 23514", err)
	}
}

func TestLinkedEligibilityRepo_ProductDeleteCascades(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Cascade Child", "cascade-child-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent := mustNewProduct(t, "Cascade Parent", "cascade-parent-"+id.New())
	if err := productRepo.Create(ctx, &parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	link, err := catalog.NewLinkedEligibleParent(child.ID, parent.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	if err := repo.InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent: %v", err)
	}

	mustExec(t, db, "DELETE FROM products WHERE id = $1", parent.ID)
	list, err := repo.ListParentsByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentsByChildID: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("parent delete should cascade, got %+v", list)
	}

	if err := productRepo.Create(ctx, &parent); err != nil {
		t.Fatalf("recreate parent: %v", err)
	}
	if err := repo.InsertParent(ctx, link); err != nil {
		t.Fatalf("InsertParent after recreate: %v", err)
	}
	mustExec(t, db, "DELETE FROM products WHERE id = $1", child.ID)
	n, err := repo.CountRulesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("CountRulesByChildID: %v", err)
	}
	if n != 0 {
		t.Fatalf("child delete should cascade, count = %d", n)
	}
}

func TestLinkedEligibilityRepo_CategoryDeleteCascades(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	categoryRepo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Cat Cascade Child", "cat-cascade-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	cat := mustNewCategory(t, "Temp Cards", "temp-cards-"+id.New())
	if err := categoryRepo.Create(ctx, &cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	rule, err := catalog.NewLinkedEligibleCategory(child.ID, cat.ID)
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}
	if err := repo.InsertCategory(ctx, rule); err != nil {
		t.Fatalf("InsertCategory: %v", err)
	}

	if err := categoryRepo.Delete(ctx, cat.ID); err != nil {
		t.Fatalf("Delete category: %v", err)
	}
	list, err := repo.ListCategoriesByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListCategoriesByChildID: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("category delete should cascade, got %+v", list)
	}
}

func TestLinkedEligibilityRepo_ListEmptyID(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	if _, err := repo.ListParentsByChildID(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ListParentsByChildID = %v, want empty id error", err)
	}
	if _, err := repo.ListCategoriesByChildID(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ListCategoriesByChildID = %v, want empty id error", err)
	}
	if _, err := repo.IsEligible(context.Background(), "", "p"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("IsEligible = %v, want empty id error", err)
	}
}

func TestLinkedEligibilityRepo_ListOrderTieBreak(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewLinkedEligibilityRepo(db)
	if err != nil {
		t.Fatalf("NewLinkedEligibilityRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	child := mustNewProduct(t, "Order Child", "order-child-"+id.New())
	if err := productRepo.Create(ctx, &child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	a := mustNewProduct(t, "Parent A", "parent-a-"+id.New())
	if err := productRepo.Create(ctx, &a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	b := mustNewProduct(t, "Parent B", "parent-b-"+id.New())
	if err := productRepo.Create(ctx, &b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	for _, parent := range []catalog.Product{a, b} {
		link, err := catalog.NewLinkedEligibleParent(child.ID, parent.ID)
		if err != nil {
			t.Fatalf("NewLinkedEligibleParent: %v", err)
		}
		if err := repo.InsertParent(ctx, link); err != nil {
			t.Fatalf("InsertParent: %v", err)
		}
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mustExec(t, db, `UPDATE linked_eligible_parents SET created_at = $1 WHERE child_product_id = $2`, ts, child.ID)

	list, err := repo.ListParentsByChildID(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentsByChildID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ParentProductID > list[1].ParentProductID {
		t.Errorf("expected ascending parent_product_id tie-break, got %s then %s",
			list[0].ParentProductID, list[1].ParentProductID)
	}
}
