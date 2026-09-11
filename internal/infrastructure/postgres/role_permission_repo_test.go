package postgres_test

import (
	"context"
	"sort"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
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
