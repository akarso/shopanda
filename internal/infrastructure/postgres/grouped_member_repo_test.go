package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGroupedMemberRepo_Migration089Applied(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	var rel sql.NullString
	if err := db.QueryRow(`SELECT to_regclass('public.grouped_members')::text`).Scan(&rel); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if !rel.Valid || rel.String != "grouped_members" {
		t.Fatalf("grouped_members missing (migration 089 not applied): %v", rel)
	}
}

func TestGroupedMemberRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()
	group := mustNewProduct(t, "Tx Group", "tx-group-"+id.New())
	group.Type = catalog.TypeGrouped
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	member := mustNewProduct(t, "Tx Member", "tx-member-"+id.New())
	if err := productRepo.Create(ctx, &member); err != nil {
		t.Fatalf("create member: %v", err)
	}
	link, err := catalog.NewGroupedMember(group.ID, member.ID)
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Insert(ctx, link); err != nil {
		t.Fatalf("Insert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	list, err := repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("rolled-back insert visible: %+v", list)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Insert(ctx, link); err != nil {
		t.Fatalf("Insert in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	list, err = repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID after commit: %v", err)
	}
	if len(list) != 1 || list[0].MemberProductID != member.ID {
		t.Fatalf("committed list = %+v", list)
	}
}

func TestGroupedMemberRepo_NilDB(t *testing.T) {
	_, err := postgres.NewGroupedMemberRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestGroupedMemberRepo_InsertListRemove(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Lookbook", "lookbook-"+id.New())
	group.Type = catalog.TypeGrouped
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	member := mustNewProduct(t, "Scarf", "scarf-"+id.New())
	if err := productRepo.Create(ctx, &member); err != nil {
		t.Fatalf("create member: %v", err)
	}

	link, err := catalog.NewGroupedMember(group.ID, member.ID)
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if err := repo.Insert(ctx, link); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.Insert(ctx, link); err != nil {
		t.Fatalf("Insert duplicate: %v", err)
	}

	list, err := repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 (insert-ignore)", len(list))
	}
	if list[0].MemberProductID != member.ID || list[0].GroupProductID != group.ID {
		t.Errorf("list[0] = %+v", list[0])
	}

	n, err := repo.CountByGroupID(ctx, group.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByGroupID = %d, %v, want 1", n, err)
	}
	n, err = repo.CountByMemberID(ctx, member.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByMemberID = %d, %v, want 1", n, err)
	}

	if err := repo.Remove(ctx, group.ID, member.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, err = repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID after remove: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %+v", list)
	}

	err = repo.Remove(ctx, group.ID, member.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Remove missing = %v, want not found", err)
	}
}

func TestGroupedMemberRepo_ListEmptyNonNil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Empty Group", "empty-group-"+id.New())
	group.Type = catalog.TypeGrouped
	productRepo, _ := postgres.NewProductRepo(db)
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}

	list, err := repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil empty slice")
	}
}

func TestGroupedMemberRepo_ListEmptyID(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	_, err = repo.ListByGroupID(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ListByGroupID = %v, want empty id error", err)
	}
}

func TestGroupedMemberRepo_InsertUnknownProduct(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	link, err := catalog.NewGroupedMember(id.New(), id.New())
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	err = repo.Insert(ctx, link)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("Insert = %v, want not found (23503)", err)
	}
}

func TestGroupedMemberRepo_InsertInvalidUUID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}

	link := &catalog.GroupedMember{GroupProductID: "not-a-uuid", MemberProductID: "also-not"}
	err = repo.Insert(context.Background(), link)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Insert = %v, want validation (22P02)", err)
	}
	if !strings.Contains(err.Error(), "invalid product id") {
		t.Fatalf("Insert = %v, want invalid product id", err)
	}
}

func TestGroupedMemberRepo_SelfReferenceCheck(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	ctx := productRepoAdminCtx()
	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	group := mustNewProduct(t, "Self Group", "self-group-"+id.New())
	group.Type = catalog.TypeGrouped
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}

	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	err = repo.Insert(ctx, &catalog.GroupedMember{GroupProductID: group.ID, MemberProductID: group.ID})
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Insert self = %v, want validation", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO grouped_members (group_product_id, member_product_id) VALUES ($1, $1)`, group.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("raw self insert = %v, want SQLSTATE 23514", err)
	}
}

func TestGroupedMemberRepo_MemberDeleteCascades(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Cascade Group", "cascade-group-"+id.New())
	group.Type = catalog.TypeGrouped
	member := mustNewProduct(t, "Cascade Member", "cascade-member-"+id.New())
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := productRepo.Create(ctx, &member); err != nil {
		t.Fatalf("create member: %v", err)
	}
	link, err := catalog.NewGroupedMember(group.ID, member.ID)
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if err := repo.Insert(ctx, link); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if _, err := db.Exec("DELETE FROM products WHERE id = $1", member.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	n, err := repo.CountByGroupID(ctx, group.ID)
	if err != nil || n != 0 {
		t.Fatalf("CountByGroupID after member delete = %d, %v, want 0 (CASCADE)", n, err)
	}
}

func TestGroupedMemberRepo_ListOrderingSameCreatedAt(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Order Group", "order-group-"+id.New())
	group.Type = catalog.TypeGrouped
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}

	idLow, idHigh := distinctOrderedUUIDs(t)
	for i, spec := range []struct {
		id, name string
	}{
		{idHigh, "Beta"},
		{idLow, "Alpha"},
	} {
		p := mustNewProduct(t, spec.name, spec.name+"-gorder-"+id.New())
		p.ID = spec.id
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("create member %d: %v", i, err)
		}
		link, err := catalog.NewGroupedMember(group.ID, p.ID)
		if err != nil {
			t.Fatalf("NewGroupedMember: %v", err)
		}
		if err := repo.Insert(ctx, link); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	ts := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	mustExec(t, db, `UPDATE grouped_members SET created_at = $1 WHERE group_product_id = $2`, ts, group.ID)

	list, err := repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].MemberProductID != idLow || list[1].MemberProductID != idHigh {
		t.Errorf("expected ascending member_product_id tie-break, got %s then %s",
			list[0].MemberProductID, list[1].MemberProductID)
	}
}

func TestGroupedMemberRepo_ListOrderingByCreatedAt(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Chrono Group", "chrono-group-"+id.New())
	group.Type = catalog.TypeGrouped
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}

	idEarly, idLate := distinctOrderedUUIDs(t)
	// idLate is the greater member_product_id. Give it the earlier created_at
	// so list order follows the timestamp rather than the id.
	for _, spec := range []struct {
		id, name string
	}{
		{idLate, "LateID"},
		{idEarly, "EarlyID"},
	} {
		p := mustNewProduct(t, spec.name, spec.name+"-gchrono-"+id.New())
		p.ID = spec.id
		if err := productRepo.Create(ctx, &p); err != nil {
			t.Fatalf("create member: %v", err)
		}
		link, err := catalog.NewGroupedMember(group.ID, p.ID)
		if err != nil {
			t.Fatalf("NewGroupedMember: %v", err)
		}
		if err := repo.Insert(ctx, link); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	earlyAt := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	lateAt := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	mustExec(t, db, `UPDATE grouped_members SET created_at = $1 WHERE member_product_id = $2`, earlyAt, idLate)
	mustExec(t, db, `UPDATE grouped_members SET created_at = $1 WHERE member_product_id = $2`, lateAt, idEarly)

	list, err := repo.ListByGroupID(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListByGroupID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].MemberProductID != idLate || list[1].MemberProductID != idEarly {
		t.Errorf("expected created_at order (later id first), got %s then %s",
			list[0].MemberProductID, list[1].MemberProductID)
	}
}

func TestGroupedMemberRepo_GroupDeleteCascades(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Parent Group", "parent-group-"+id.New())
	group.Type = catalog.TypeGrouped
	memberA := mustNewProduct(t, "Member A", "member-a-"+id.New())
	memberB := mustNewProduct(t, "Member B", "member-b-"+id.New())
	for _, p := range []*catalog.Product{&group, &memberA, &memberB} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.Name, err)
		}
	}
	for _, memberID := range []string{memberA.ID, memberB.ID} {
		link, err := catalog.NewGroupedMember(group.ID, memberID)
		if err != nil {
			t.Fatalf("NewGroupedMember: %v", err)
		}
		if err := repo.Insert(ctx, link); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	if _, err := db.Exec("DELETE FROM products WHERE id = $1", group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	for _, memberID := range []string{memberA.ID, memberB.ID} {
		n, err := repo.CountByMemberID(ctx, memberID)
		if err != nil || n != 0 {
			t.Fatalf("CountByMemberID(%s) after group delete = %d, %v, want 0", memberID, n, err)
		}
		got, err := productRepo.FindByID(ctx, memberID)
		if err != nil {
			t.Fatalf("FindByID member: %v", err)
		}
		if got == nil || got.ID != memberID {
			t.Fatalf("member product missing after group delete: %+v", got)
		}
	}
	if n, err := repo.CountByGroupID(ctx, group.ID); err != nil || n != 0 {
		t.Fatalf("CountByGroupID after group delete = %d, %v, want 0", n, err)
	}
	if got, err := productRepo.FindByID(ctx, group.ID); err != nil || got != nil {
		t.Fatalf("group product still present: %+v, %v", got, err)
	}
}

func TestGroupedMemberRepo_MemberInTwoGroups(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	repo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	bundleRepo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		t.Fatalf("NewBundleComponentRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	g1 := mustNewProduct(t, "Group One", "group-one-"+id.New())
	g1.Type = catalog.TypeGrouped
	g2 := mustNewProduct(t, "Group Two", "group-two-"+id.New())
	g2.Type = catalog.TypeGrouped
	member := mustNewProduct(t, "Shared Member", "shared-member-"+id.New())
	for _, p := range []*catalog.Product{&g1, &g2, &member} {
		if err := productRepo.Create(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.Name, err)
		}
	}
	for _, groupID := range []string{g1.ID, g2.ID} {
		link, err := catalog.NewGroupedMember(groupID, member.ID)
		if err != nil {
			t.Fatalf("NewGroupedMember: %v", err)
		}
		if err := repo.Insert(ctx, link); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	for _, groupID := range []string{g1.ID, g2.ID} {
		list, err := repo.ListByGroupID(ctx, groupID)
		if err != nil {
			t.Fatalf("ListByGroupID: %v", err)
		}
		if len(list) != 1 || list[0].MemberProductID != member.ID {
			t.Fatalf("list %s = %+v, want member %s", groupID, list, member.ID)
		}
	}
	n, err := repo.CountByMemberID(ctx, member.ID)
	if err != nil || n != 2 {
		t.Fatalf("CountByMemberID = %d, %v, want 2", n, err)
	}

	h := admin.NewProductAdminHandler(productRepo, event.NewBus(logger.New("error"))).
		WithBundleComponents(bundleRepo).
		WithGroupedMembers(repo).
		WithTxStarter(db)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/products/{id}", h.Update())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/"+member.ID, strings.NewReader(`{"type":"bundle"}`))
	req = req.WithContext(catalog.WithOperatorProductReadScope(req.Context()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "used as a grouped member") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	got, err := productRepo.FindByID(ctx, member.ID)
	if err != nil || got == nil || got.Type != catalog.TypeSimple {
		t.Fatalf("member after rejected type change = %+v, %v", got, err)
	}
}

// TestGroupedMemberRepo_AddLockBlocksTypeChange holds FOR UPDATE and an
// uncommitted membership insert, then a second connection's product lock
// (the same lock a type change takes) waits and fails with lock_timeout.
// The in-memory ordering is TestGroupedService_Add_LocksBeforeInsert and
// TestProductAdminHandler_Update_TypeChangeUsesLockedGroupedType.
func TestGroupedMemberRepo_AddLockBlocksTypeChange(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	memberRepo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		t.Fatalf("NewGroupedMemberRepo: %v", err)
	}
	ctx := productRepoAdminCtx()

	group := mustNewProduct(t, "Lock Group", "lock-group-"+id.New())
	group.Type = catalog.TypeGrouped
	member := mustNewProduct(t, "Lock Member", "lock-member-"+id.New())
	if err := productRepo.Create(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := productRepo.Create(ctx, &member); err != nil {
		t.Fatalf("create member: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := productRepo.WithTx(tx).(*postgres.ProductRepo).LockForUpdate(ctx, group.ID); err != nil {
		t.Fatalf("LockForUpdate: %v", err)
	}
	link, err := catalog.NewGroupedMember(group.ID, member.ID)
	if err != nil {
		t.Fatalf("NewGroupedMember: %v", err)
	}
	if err := memberRepo.WithTx(tx).Insert(ctx, link); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx second: %v", err)
	}
	defer tx2.Rollback()
	if _, err := tx2.ExecContext(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	start := time.Now()
	_, err = productRepo.WithTx(tx2).(*postgres.ProductRepo).LockForUpdate(ctx, group.ID)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("type-change lock acquired while the membership transaction holds FOR UPDATE")
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("lock returned in %s, want a wait then failure: %v", elapsed, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Fatalf("LockForUpdate = %v, want lock timeout", err)
	}
}
