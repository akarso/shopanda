package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

// TestRolePermissionRepo_ListAll_MatchesCompiledCoreDefaults pins a real,
// pre-existing bug: role_permissions is seeded exactly once, in
// 058_create_role_permissions.sql, and no later migration ever added a
// row for a permission added to internal/domain/rbac/role_permissions.go's
// compiled defaults afterwards (customers.store_credit.write,
// extensions.read/write/private.read, jobs.read/write, search.reindex —
// all admin-only). Once rbac.InitEffectivePermissions has run (which
// happens on every server startup, via adminrole.Service.
// SyncPluginDefaults), HasPermission uses this table's contents
// *exclusively* for admin roles — it does not fall back to the compiled
// map for a permission missing from the table. So every one of those
// permissions was silently unenforceable for every admin, in every
// deployment, from the moment it was added to the compiled defaults,
// unless an operator happened to add the matching row by hand.
//
// This test asserts a freshly migrated database's role_permissions table
// (before any SyncPluginDefaults/EnsurePermissions call, i.e. core-only)
// grants each admin-level role exactly its compiled core defaults — no
// more, no less. It both pins migration 075's backfill and guards against
// the same class of gap recurring: a future permission added to
// rolePermissions.go without a matching migration row fails this test
// immediately, rather than silently 403ing in production.
func TestRolePermissionRepo_ListAll_MatchesCompiledCoreDefaults(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo, err := postgres.NewRolePermissionRepo(db)
	if err != nil {
		t.Fatalf("NewRolePermissionRepo: %v", err)
	}

	assignments, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	for _, role := range rbac.AdminRoles() {
		want := sortedStrings(rbac.DefaultPermissionsForRole(role))
		got := sortedStrings(assignments[role])
		if !equalStrings(want, got) {
			t.Errorf("role %s: role_permissions table = %v, want compiled core defaults %v", role, got, want)
		}
	}
}

// backfilledAdminPermissions is the exact set 075 added for role='admin'
// — kept here as a literal (not a reference to rbac constants) so this
// test independently pins the specific rows 076 is scoped to, rather than
// silently tracking whatever the compiled defaults become later.
var backfilledAdminPermissions = []string{
	"customers.store_credit.write",
	"extensions.read",
	"extensions.write",
	"extensions.private.read",
	"jobs.read",
	"jobs.write",
	"search.reindex",
}

const migration075Name = "075_backfill_role_permissions_core_gaps.sql"

// resetRoleBackfillState puts the database into a known baseline before
// each of the three tests below: all 7 backfilled admin permissions
// present, and no role.update audit history for the admin role. Tests in
// this package share one physical database (SHOPANDA_TEST_DSN, not a
// schema-per-test), and migrate.Run only ever applies 075/076 once across
// the whole run — so these tests exercise 076's own DELETE statement
// directly (via rerunMigration076), against state they arrange
// themselves, rather than relying on migrate.Run to re-apply it under
// different historical conditions.
func resetRoleBackfillState(t *testing.T, db *sql.DB) {
	t.Helper()
	restoreBackfillBaseline := func() {
		for _, perm := range backfilledAdminPermissions {
			if _, err := db.Exec(`INSERT INTO role_permissions (role, permission) VALUES ('admin', $1) ON CONFLICT DO NOTHING`, perm); err != nil {
				t.Fatalf("reset role_permissions: %v", err)
			}
		}
		if _, err := db.Exec(`DELETE FROM admin_audit_log WHERE resource_type = 'admin_role' AND resource_id = 'admin'`); err != nil {
			t.Fatalf("reset admin_audit_log: %v", err)
		}
	}
	restoreBackfillBaseline()
	// Leave the shared test database exactly as this test found it, so
	// other tests in this package (and this one, if re-run) don't inherit
	// whatever mutated state this test's own body produces.
	t.Cleanup(restoreBackfillBaseline)
}

// TestMigration076_LeavesBackfillAlone_WhenAdminNeverCustomized covers the
// common case: no operator has ever called PUT /api/v1/admin/roles/admin,
// so 075's insert was a pure bug fix and 076 must not touch it.
func TestMigration076_LeavesBackfillAlone_WhenAdminNeverCustomized(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	resetRoleBackfillState(t, db)

	if err := rerunMigration076(t, db); err != nil {
		t.Fatalf("re-run 076: %v", err)
	}

	got := adminPermissions(t, db)
	for _, perm := range backfilledAdminPermissions {
		if !contains(got, perm) {
			t.Errorf("admin permissions = %v, want %q present (no customization ever happened, so 076 must leave 075's backfill in place)", got, perm)
		}
	}
}

// TestMigration076_RevertsBackfill_WhenAdminWasCustomizedBeforeIt pins
// 076's own logic in isolation: an operator explicitly configured the
// admin role (audit entry recorded) *before* 075 ran, so 075's insert
// silently overrode that choice, and 076 deletes all 7 backfilled
// permissions in response. This is 076's actual behavior, and it is
// itself a bug — 076 cannot tell "the operator's update omitted all 7"
// apart from "omitted some, deliberately retained others" (the audit
// trail only ever records a permission count, never which permissions
// were granted), so it deletes indiscriminately. A real deployment never
// observes this in isolation, though: 077 always runs immediately after
// and reverses it under the identical guard — see
// TestMigration077_RestoresBackfill_WhenAdminWasCustomizedBeforeIt for
// the actual net, end-to-end outcome.
func TestMigration076_RevertsBackfill_WhenAdminWasCustomizedBeforeIt(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	resetRoleBackfillState(t, db)

	var appliedAt time.Time
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = $1`, migration075Name).Scan(&appliedAt); err != nil {
		t.Fatalf("read 075 applied_at: %v", err)
	}

	// The operator had already explicitly saved the admin role's
	// permissions *before* 075 ran (e.g. during the same maintenance
	// window, or any time prior) — recorded here as an audit entry dated
	// before 075's applied_at.
	insertRoleUpdateAudit(t, db, appliedAt.Add(-1*time.Hour))

	if err := rerunMigration076(t, db); err != nil {
		t.Fatalf("re-run 076: %v", err)
	}

	got := adminPermissions(t, db)
	for _, perm := range backfilledAdminPermissions {
		if contains(got, perm) {
			t.Errorf("admin permissions = %v, want %q removed (076 should revert 075's backfill: admin was already explicitly customized before 075 ran)", got, perm)
		}
	}
}

// TestMigration076_LeavesBackfillAlone_WhenAdminWasCustomizedAfterIt
// covers the other side: an operator saved the admin role *after* 075 ran
// (so the current row set already reflects their explicit, current
// intent — including, in this test, deliberately keeping search.reindex
// but not the others). 076 must not touch anything in this case; the
// later save is authoritative regardless of what it contains.
func TestMigration076_LeavesBackfillAlone_WhenAdminWasCustomizedAfterIt(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	resetRoleBackfillState(t, db)

	var appliedAt time.Time
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = $1`, migration075Name).Scan(&appliedAt); err != nil {
		t.Fatalf("read 075 applied_at: %v", err)
	}

	// Simulate a real ReplaceForRole call made after 075 ran: the
	// operator's save kept search.reindex but dropped every other
	// backfilled permission.
	if _, err := db.Exec(`DELETE FROM role_permissions WHERE role = 'admin' AND permission = ANY($1::text[])`, toPQArray(backfilledAdminPermissions)); err != nil {
		t.Fatalf("simulate replace: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO role_permissions (role, permission) VALUES ('admin', 'search.reindex')`); err != nil {
		t.Fatalf("simulate replace insert: %v", err)
	}
	insertRoleUpdateAudit(t, db, appliedAt.Add(1*time.Hour))

	if err := rerunMigration076(t, db); err != nil {
		t.Fatalf("re-run 076: %v", err)
	}

	got := adminPermissions(t, db)
	if !contains(got, "search.reindex") {
		t.Fatalf("admin permissions = %v, want search.reindex kept (operator's post-075 save is authoritative)", got)
	}
	for _, perm := range backfilledAdminPermissions {
		if perm == "search.reindex" {
			continue
		}
		if contains(got, perm) {
			t.Errorf("admin permissions = %v, want %q absent (operator's post-075 save dropped it)", got, perm)
		}
	}
}

func adminPermissions(t *testing.T, db *sql.DB) []string {
	t.Helper()
	repo, err := postgres.NewRolePermissionRepo(db)
	if err != nil {
		t.Fatalf("NewRolePermissionRepo: %v", err)
	}
	assignments, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	return sortedStrings(assignments[identity.RoleAdmin])
}

func insertRoleUpdateAudit(t *testing.T, db *sql.DB, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO admin_audit_log (id, created_at, admin_id, action, resource_type, resource_id, result)
		 VALUES ($1, $2, 'test-admin', 'role.update', 'admin_role', 'admin', 'success')`,
		id.New(), createdAt,
	)
	if err != nil {
		t.Fatalf("insert role.update audit entry: %v", err)
	}
}

func rerunMigration076(t *testing.T, db *sql.DB) error {
	t.Helper()
	content, err := os.ReadFile("../../../migrations/076_scope_role_permissions_backfill_to_uncustomized_admin.sql")
	if err != nil {
		return err
	}
	_, err = db.Exec(string(content))
	return err
}

func rerunMigration077(t *testing.T, db *sql.DB) error {
	t.Helper()
	content, err := os.ReadFile("../../../migrations/077_undo_076_role_permission_blanket_revoke.sql")
	if err != nil {
		return err
	}
	_, err = db.Exec(string(content))
	return err
}

// TestMigration077_RestoresBackfill_WhenAdminWasCustomizedBeforeIt is the
// code review fix for 076 itself: 076's own guard could not tell "the
// operator's pre-075 update omitted all 7 permissions" apart from
// "omitted some, deliberately retained others" — adminrole.Service.
// UpdateRole's audit entry only ever records a permission *count*, never
// which permissions were granted. So 076 deleted all 7 whenever its
// guard fired, which would have silently revoked any of the 7 a real
// update had actually kept. 077 corrects this by reversing 076's
// deletion under the exact same guard conditions, since that historical
// ambiguity can never be resolved precisely (no other table records
// role_permissions history, and a pre-075 update — by definition —
// predates any possible audit enrichment). This test exercises the same
// "customized only before 075" scenario as
// TestMigration076_RevertsBackfill_WhenAdminWasCustomizedBeforeIt, but
// carries it through 077 too, and asserts the net, real-world outcome:
// all 7 permissions present, i.e. 076's deletion never actually took
// effect once 077 also runs.
func TestMigration077_RestoresBackfill_WhenAdminWasCustomizedBeforeIt(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	resetRoleBackfillState(t, db)

	var appliedAt time.Time
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = $1`, migration075Name).Scan(&appliedAt); err != nil {
		t.Fatalf("read 075 applied_at: %v", err)
	}
	insertRoleUpdateAudit(t, db, appliedAt.Add(-1*time.Hour))

	if err := rerunMigration076(t, db); err != nil {
		t.Fatalf("re-run 076: %v", err)
	}
	if err := rerunMigration077(t, db); err != nil {
		t.Fatalf("re-run 077: %v", err)
	}

	got := adminPermissions(t, db)
	for _, perm := range backfilledAdminPermissions {
		if !contains(got, perm) {
			t.Errorf("admin permissions = %v, want %q present (077 must restore whatever 076 deleted under ambiguous pre-075-only history)", got, perm)
		}
	}
}

// TestMigration077_LeavesLaterCustomizationAlone_WhenAdminWasCustomizedAfterIt
// pins that 077 does not reopen the case 076 already handled correctly:
// when the admin role was explicitly saved *after* 075 ran, that save is
// authoritative and untouched by either 076 or 077, no matter what it
// contains.
func TestMigration077_LeavesLaterCustomizationAlone_WhenAdminWasCustomizedAfterIt(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	resetRoleBackfillState(t, db)

	var appliedAt time.Time
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = $1`, migration075Name).Scan(&appliedAt); err != nil {
		t.Fatalf("read 075 applied_at: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM role_permissions WHERE role = 'admin' AND permission = ANY($1::text[])`, toPQArray(backfilledAdminPermissions)); err != nil {
		t.Fatalf("simulate replace: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO role_permissions (role, permission) VALUES ('admin', 'search.reindex')`); err != nil {
		t.Fatalf("simulate replace insert: %v", err)
	}
	insertRoleUpdateAudit(t, db, appliedAt.Add(1*time.Hour))

	if err := rerunMigration076(t, db); err != nil {
		t.Fatalf("re-run 076: %v", err)
	}
	if err := rerunMigration077(t, db); err != nil {
		t.Fatalf("re-run 077: %v", err)
	}

	got := adminPermissions(t, db)
	if !contains(got, "search.reindex") {
		t.Fatalf("admin permissions = %v, want search.reindex kept (operator's post-075 save is authoritative)", got)
	}
	for _, perm := range backfilledAdminPermissions {
		if perm == "search.reindex" {
			continue
		}
		if contains(got, perm) {
			t.Errorf("admin permissions = %v, want %q absent (077 must not restore what the operator's post-075 save dropped)", got, perm)
		}
	}
}

func toPQArray(ss []string) string {
	out := "{"
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += `"` + s + `"`
	}
	return out + "}"
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func sortedStrings(perms []rbac.Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
