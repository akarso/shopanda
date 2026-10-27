package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/jackc/pgx/v5/pgconn"
)

func ensureProductAssignmentsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM product_assignments")
	})
}

func TestProductAssignmentRepo_Migration099Applied(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)

	var rel sql.NullString
	if err := db.QueryRow(`SELECT to_regclass($1)::text`, "public.product_assignments").Scan(&rel); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if !rel.Valid || rel.String != "product_assignments" {
		t.Fatalf("product_assignments missing (migration 099 not applied): %v", rel)
	}
}

func TestProductAssignmentRepo_NilDB(t *testing.T) {
	_, err := postgres.NewProductAssignmentRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestProductAssignmentRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	child := "child-" + id.New()
	a, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Insert(ctx, a); err != nil {
		t.Fatalf("Insert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, err := repo.FindByID(ctx, a.ID)
	if err != nil || got != nil {
		t.Fatalf("rolled-back insert visible: %+v %v", got, err)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Insert(ctx, a); err != nil {
		t.Fatalf("Insert in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err = repo.FindByID(ctx, a.ID)
	if err != nil || got == nil || got.ChildOrderItemID != child {
		t.Fatalf("committed FindByID = %+v %v", got, err)
	}
}

func TestProductAssignmentRepo_InsertFindListAssignRevoke(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()

	child := "child-" + id.New()
	parent := "parent-" + id.New()
	u, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, u); err != nil {
		t.Fatalf("Insert unassigned: %v", err)
	}

	got, err := repo.FindByID(ctx, u.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID = %v %v", got, err)
	}
	if got.Status() != assignment.StatusUnassigned || got.ChildOrderItemID != child || got.ParentOrderItemID != "" {
		t.Fatalf("loaded = %+v status=%q", got, got.Status())
	}

	current, err := repo.FindCurrentByChildID(ctx, child)
	if err != nil || current == nil || current.ID != u.ID {
		t.Fatalf("FindCurrentByChildID = %+v %v", current, err)
	}

	if err := got.Assign(parent); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update assign: %v", err)
	}
	if got.StoredStatus() != assignment.StatusAssigned {
		t.Fatalf("stored after update = %q", got.StoredStatus())
	}

	assigned, err := repo.FindByID(ctx, u.ID)
	if err != nil || assigned == nil || assigned.Status() != assignment.StatusAssigned || assigned.ParentOrderItemID != parent {
		t.Fatalf("after assign = %+v %v", assigned, err)
	}

	if err := assigned.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, assigned); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}
	revoked, err := repo.FindByID(ctx, u.ID)
	if err != nil || revoked == nil || revoked.Status() != assignment.StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("after revoke = %+v %v", revoked, err)
	}

	none, err := repo.FindCurrentByChildID(ctx, child)
	if err != nil || none != nil {
		t.Fatalf("current after revoke = %+v %v", none, err)
	}

	next, err := assignment.NewAssigned(id.New(), parent+"-2", child)
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	if err := repo.Insert(ctx, next); err != nil {
		t.Fatalf("Insert after revoke: %v", err)
	}

	list, err := repo.ListByChildID(ctx, child)
	if err != nil {
		t.Fatalf("ListByChildID: %v", err)
	}
	if list == nil || len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ID != next.ID || list[1].ID != u.ID {
		t.Fatalf("list order = %s,%s want newest first", list[0].ID, list[1].ID)
	}
}

func TestProductAssignmentRepo_UniqueCurrentChild(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	child := "child-" + id.New()

	first, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	second, err := assignment.NewAssigned(id.New(), "parent-"+id.New(), child)
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	err = repo.Insert(ctx, second)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("second current = %v, want conflict", err)
	}
}

func TestProductAssignmentRepo_StatusConflict(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()

	a, err := assignment.NewUnassigned(id.New(), "child-"+id.New())
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	first, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	second, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID second: %v", err)
	}
	if err := first.Assign("parent-" + id.New()); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("Update first: %v", err)
	}
	if err := second.Assign("parent-" + id.New()); err != nil {
		t.Fatalf("Assign second: %v", err)
	}
	err = repo.Update(ctx, second)
	if !errors.Is(err, assignment.ErrStatusConflict) {
		t.Fatalf("stale Update = %v, want ErrStatusConflict", err)
	}
}

func TestProductAssignmentRepo_SelfLinkConstraint(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)

	_, err := db.Exec(`INSERT INTO product_assignments
		(id, parent_order_item_id, child_order_item_id, status, created_at)
		VALUES ($1, $2, $2, 'assigned', now())`, id.New(), "same-unit")
	if err == nil {
		t.Fatal("expected self-link CHECK (23514)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("self insert = %v, want 23514", err)
	}
}

func TestProductAssignmentRepo_ParentRewriteRejected(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	parent := "parent-" + id.New()

	assigned, err := assignment.NewAssigned(id.New(), parent, "child-"+id.New())
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	if err := repo.Insert(ctx, assigned); err != nil {
		t.Fatalf("Insert assigned: %v", err)
	}
	loaded, err := repo.FindByID(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	loaded.ParentOrderItemID = "other-" + id.New()
	err = repo.Update(ctx, loaded)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("assigned parent rewrite = %v, want validation", err)
	}
	again, err := repo.FindByID(ctx, assigned.ID)
	if err != nil || again == nil || again.ParentOrderItemID != parent {
		t.Fatalf("assigned stored parent = %+v %v, want %s", again, err, parent)
	}

	if err := again.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, again); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}
	revoked, err := repo.FindByID(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("FindByID revoked: %v", err)
	}
	revoked.ParentOrderItemID = "other-" + id.New()
	err = repo.Update(ctx, revoked)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("revoked parent rewrite = %v, want validation", err)
	}
	stored, err := repo.FindByID(ctx, assigned.ID)
	if err != nil || stored == nil || stored.ParentOrderItemID != parent {
		t.Fatalf("revoked stored parent = %+v %v, want %s", stored, err, parent)
	}
}

func TestProductAssignmentRepo_RevokeUnassigned(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	child := "child-" + id.New()
	u, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, u); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	loaded, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := loaded.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.FindByID(ctx, u.ID)
	if err != nil || got == nil || got.Status() != assignment.StatusRevoked || got.ParentOrderItemID != "" || got.RevokedAt == nil {
		t.Fatalf("revoked unassigned = %+v %v", got, err)
	}
	current, err := repo.FindCurrentByChildID(ctx, child)
	if err != nil || current != nil {
		t.Fatalf("current after unassigned revoke = %+v %v", current, err)
	}
}

func TestProductAssignmentRepo_FindByID_InvalidUUID(t *testing.T) {
	repo, err := postgres.NewProductAssignmentRepo(&sql.DB{})
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	_, err = repo.FindByID(context.Background(), "nope")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("FindByID nope = %v, want validation", err)
	}
}

func TestProductAssignmentRepo_FindMissing(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	got, err := repo.FindByID(ctx, id.New())
	if err != nil || got != nil {
		t.Fatalf("FindByID missing = %v %v", got, err)
	}
	current, err := repo.FindCurrentByChildID(ctx, "no-such-child")
	if err != nil || current != nil {
		t.Fatalf("FindCurrent missing = %v %v", current, err)
	}
	list, err := repo.ListByChildID(ctx, "no-such-child")
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("List missing = %v %v", list, err)
	}
	byParent, hasMore, err := repo.ListByParentID(ctx, "no-such-parent", assignment.ParentPage{Status: "all", Limit: 100})
	if err != nil || hasMore || byParent == nil || len(byParent) != 0 {
		t.Fatalf("ListByParentID missing = %v hasMore=%v %v", byParent, hasMore, err)
	}
}

func TestProductAssignmentRepo_ListByParentID_EmptyID(t *testing.T) {
	repo, err := postgres.NewProductAssignmentRepo(&sql.DB{})
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	_, _, err = repo.ListByParentID(context.Background(), "", assignment.ParentPage{})
	if err == nil {
		t.Fatal("expected error for empty parent id")
	}
}

func TestProductAssignmentRepo_ListByParentID(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	parent := "parent-" + id.New()
	otherParent := "other-" + id.New()
	childA := "child-a-" + id.New()
	childB := "child-b-" + id.New()
	childUnassigned := "child-u-" + id.New()

	older, err := assignment.NewAssigned(id.New(), parent, childA)
	if err != nil {
		t.Fatalf("NewAssigned older: %v", err)
	}
	newer, err := assignment.NewAssigned(id.New(), parent, childB)
	if err != nil {
		t.Fatalf("NewAssigned newer: %v", err)
	}
	newer.CreatedAt = older.CreatedAt.Add(time.Second)
	other, err := assignment.NewAssigned(id.New(), otherParent, "child-other-"+id.New())
	if err != nil {
		t.Fatalf("NewAssigned other: %v", err)
	}
	unassigned, err := assignment.NewUnassigned(id.New(), childUnassigned)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	for _, row := range []*assignment.ProductAssignment{older, newer, other, unassigned} {
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("Insert %s: %v", row.ID, err)
		}
	}
	loaded, err := repo.FindByID(ctx, older.ID)
	if err != nil || loaded == nil {
		t.Fatalf("FindByID older: %v %v", loaded, err)
	}
	if err := loaded.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}

	list, hasMore, err := repo.ListByParentID(ctx, parent, assignment.ParentPage{Status: "all", Limit: 100})
	if err != nil || hasMore {
		t.Fatalf("ListByParentID: hasMore=%v %v", hasMore, err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want assigned + revoked for the parent", len(list))
	}
	page, hasMore, err := repo.ListByParentID(ctx, parent, assignment.ParentPage{Status: "assigned", Limit: 1})
	if err != nil || hasMore || len(page) != 1 || page[0].Status() != assignment.StatusAssigned {
		t.Fatalf("assigned page = %+v hasMore=%v %v", page, hasMore, err)
	}
	pagedAll, hasMore, err := repo.ListByParentID(ctx, parent, assignment.ParentPage{Status: "all", Limit: 1})
	if err != nil || !hasMore || len(pagedAll) != 1 {
		t.Fatalf("all page = %+v hasMore=%v %v", pagedAll, hasMore, err)
	}
	if list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Fatalf("order = %s,%s want newer then revoked", list[0].ID, list[1].ID)
	}
	if list[0].Status() != assignment.StatusAssigned || list[1].Status() != assignment.StatusRevoked {
		t.Fatalf("statuses = %s,%s", list[0].Status(), list[1].Status())
	}
}

func TestProductAssignmentRepo_ListByParentID_UsesParentIndex(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	parent := "parent-" + id.New()
	row, err := assignment.NewAssigned(id.New(), parent, "child-"+id.New())
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("enable_seqscan: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN SELECT id, parent_order_item_id, child_order_item_id, status, created_at, revoked_at, forced_at, forced_reason
		FROM product_assignments
		WHERE parent_order_item_id = $1
		  AND ($2 = 'all' OR status = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4`, parent, "assigned", 101, 0)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	if !strings.Contains(plan.String(), "product_assignments_parent") {
		t.Fatalf("plan =\n%s\nwant index product_assignments_parent", plan.String())
	}
}

func TestProductAssignmentRepo_ForcedRoundTrip(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	child := "child-" + id.New()
	parent := "parent-" + id.New()
	reason := "correct a cancelled ticket"
	row, err := assignment.NewAssigned(id.New(), parent, child)
	if err != nil {
		t.Fatalf("NewAssigned: %v", err)
	}
	forcedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := row.SetForced(reason, forcedAt); err != nil {
		t.Fatalf("SetForced: %v", err)
	}
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert forced: %v", err)
	}

	got, err := repo.FindByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID = %+v %v", got, err)
	}
	if !got.Forced() || got.ForcedReason != reason || got.ForcedAt == nil || !got.ForcedAt.Equal(forcedAt) {
		t.Fatalf("loaded force at=%v reason=%q", got.ForcedAt, got.ForcedReason)
	}

	if err := got.Revoke(); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}
	revoked, err := repo.FindByID(ctx, row.ID)
	if err != nil || revoked == nil {
		t.Fatalf("FindByID after revoke = %+v %v", revoked, err)
	}
	if revoked.Status() != assignment.StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("after revoke status=%s revoked_at=%v", revoked.Status(), revoked.RevokedAt)
	}
	if !revoked.Forced() || revoked.ForcedReason != reason || revoked.ForcedAt == nil || !revoked.ForcedAt.Equal(forcedAt) {
		t.Fatalf("force did not survive revoke at=%v reason=%q", revoked.ForcedAt, revoked.ForcedReason)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO product_assignments
		(id, parent_order_item_id, child_order_item_id, status, created_at, forced_at, forced_reason)
		VALUES ($1, NULL, $2, 'unassigned', $3, $4, $5)`,
		id.New(), "child-"+id.New(), time.Now().UTC(), time.Now().UTC(), "should fail")
	assertForcedCheck(t, err)

	plain, err := assignment.NewUnassigned(id.New(), "child-"+id.New())
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, plain); err != nil {
		t.Fatalf("Insert unassigned: %v", err)
	}
	_, err = db.ExecContext(ctx, `UPDATE product_assignments
		SET forced_at = $2, forced_reason = $3
		WHERE id = $1`, plain.ID, time.Now().UTC(), "should fail")
	assertForcedCheck(t, err)

	exact := strings.Repeat("é", assignment.MaxForcedReasonLen)
	exactID := id.New()
	_, err = db.ExecContext(ctx, `INSERT INTO product_assignments
		(id, parent_order_item_id, child_order_item_id, status, created_at, forced_at, forced_reason)
		VALUES ($1, $2, $3, 'assigned', $4, $5, $6)`,
		exactID, "parent-"+id.New(), "child-"+id.New(), time.Now().UTC(), time.Now().UTC(), exact)
	if err != nil {
		t.Fatalf("insert %d-char reason: %v", assignment.MaxForcedReasonLen, err)
	}
	loaded, err := repo.FindByID(ctx, exactID)
	if err != nil || loaded == nil || loaded.ForcedReason != exact {
		t.Fatalf("scan %d-char reason = %+v %v", assignment.MaxForcedReasonLen, loaded, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO product_assignments
		(id, parent_order_item_id, child_order_item_id, status, created_at, forced_at, forced_reason)
		VALUES ($1, $2, $3, 'assigned', $4, $5, $6)`,
		id.New(), "parent-"+id.New(), "child-"+id.New(), time.Now().UTC(), time.Now().UTC(), exact+"é")
	assertForcedReasonLen(t, err)
}

func TestProductAssignmentRepo_ForcedAssignAfterCheckout(t *testing.T) {
	db := testDB(t)
	ensureProductAssignmentsTable(t, db)
	repo, err := postgres.NewProductAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewProductAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	child := "child-" + id.New()
	parent := "parent-" + id.New()
	row, err := assignment.NewUnassigned(id.New(), child)
	if err != nil {
		t.Fatalf("NewUnassigned: %v", err)
	}
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("CreateUnassigned insert: %v", err)
	}

	if err := row.Assign(parent); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	reason := "support correction after checkout"
	forcedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := row.SetForced(reason, forcedAt); err != nil {
		t.Fatalf("SetForced: %v", err)
	}
	if err := repo.Update(ctx, row); err != nil {
		t.Fatalf("Update assign+force: %v", err)
	}

	got, err := repo.FindByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID = %+v %v", got, err)
	}
	if got.Status() != assignment.StatusAssigned || got.ParentOrderItemID != parent || got.ChildOrderItemID != child {
		t.Fatalf("assigned row = status:%s parent:%q child:%q", got.Status(), got.ParentOrderItemID, got.ChildOrderItemID)
	}
	if !got.Forced() || got.ForcedReason != reason || got.ForcedAt == nil || !got.ForcedAt.Equal(forcedAt) {
		t.Fatalf("forced columns at=%v reason=%q", got.ForcedAt, got.ForcedReason)
	}
}

func assertForcedCheck(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "product_assignments_forced_check" {
		t.Fatalf("unassigned+forced = %v, want product_assignments_forced_check", err)
	}
}

func assertForcedReasonLen(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "product_assignments_forced_reason_len_check" {
		t.Fatalf("over-long forced_reason = %v, want product_assignments_forced_reason_len_check", err)
	}
}
