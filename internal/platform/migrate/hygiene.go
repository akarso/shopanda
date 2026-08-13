package migrate

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// migrationNamePattern matches the required migration filename shape: a
// leading numeric prefix, an underscore, a description, and a .sql suffix.
// The numeric prefix is what makes lexicographic filename sort (listMigrations)
// match migration application order.
var migrationNamePattern = regexp.MustCompile(`^([0-9]+)_.*\.sql$`)

// allowedPrefixCollisions is the explicit, filename-exact allowlist for the one
// historical numeric-prefix collision under migrations/. Do NOT add entries
// here to resolve a new collision — renumber the new file instead. Renaming,
// removing, or adding a file at an already-allowlisted prefix requires an
// explicit policy-change PR (these filenames are recorded verbatim in
// deployed schema_migrations tables; renaming one desyncs tracking).
var allowedPrefixCollisions = map[int][]string{
	25: {"025_add_cart_merged_guest_id.sql", "025_create_invoices.sql"},
}

// CheckFilenameHygiene validates migration filenames under dir against the
// project's naming policy (documented in DEPLOYMENT.md):
//
//   - every *.sql file must match ^([0-9]+)_.*\.sql$
//   - every numeric prefix must have the same digit width as the others
//     (e.g. all 3 digits) — listMigrations orders migrations by plain
//     lexicographic filename sort, so a shorter/longer prefix (e.g. "64_"
//     next to "065_") would sort out of numeric order and can execute early
//   - normalized numeric prefixes (leading zeros stripped) must be unique
//     across files, except for the exact historical collision recorded in
//     allowedPrefixCollisions
//   - every allowlisted historical filename must still be present unchanged
//     (independent of which prefixes are currently populated)
//   - an allowlisted prefix must contain exactly its allowlisted filenames —
//     no silent rename, removal, or addition
func CheckFilenameHygiene(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("migrate: read dir %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	return checkFilenameHygiene(names)
}

func checkFilenameHygiene(names []string) error {
	byPrefix := make(map[int][]string)
	nameSet := make(map[string]struct{}, len(names))
	prefixWidth := -1
	widthReference := ""
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		nameSet[name] = struct{}{}
		m := migrationNamePattern.FindStringSubmatch(name)
		if m == nil {
			return fmt.Errorf("migrate: %q does not match the required migration filename pattern ^([0-9]+)_.*\\.sql$ (a numeric prefix is required so filename sort order matches migration order)", name)
		}
		digits := m[1]
		if prefixWidth == -1 {
			prefixWidth = len(digits)
			widthReference = name
		} else if len(digits) != prefixWidth {
			return fmt.Errorf("migrate: %q has a %d-digit numeric prefix but %q has %d digits — mixed-width prefixes break lexicographic filename sort order (listMigrations sorts filenames as strings, not numbers), so a shorter/longer prefix can execute out of numeric order; zero-pad the prefix to match", name, len(digits), widthReference, prefixWidth)
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return fmt.Errorf("migrate: %q has an unparseable numeric prefix: %w", name, err)
		}
		byPrefix[n] = append(byPrefix[n], name)
	}

	// Allowlist membership is independent of which prefixes are currently on
	// disk: renaming/deleting both historical 025_* files must still fail even
	// when prefix 25 is empty (otherwise schema_migrations desyncs on deploy).
	allowPrefixes := make([]int, 0, len(allowedPrefixCollisions))
	for p := range allowedPrefixCollisions {
		allowPrefixes = append(allowPrefixes, p)
	}
	sort.Ints(allowPrefixes)
	for _, p := range allowPrefixes {
		wanted := append([]string(nil), allowedPrefixCollisions[p]...)
		sort.Strings(wanted)
		for _, w := range wanted {
			if _, ok := nameSet[w]; !ok {
				return fmt.Errorf("migrate: allowlisted historical migration %q (prefix %d) is missing or was renamed — these filenames are recorded in schema_migrations; renaming/removing them requires an explicit policy-change PR (wanted exact set %v)", w, p, wanted)
			}
		}
	}

	prefixes := make([]int, 0, len(byPrefix))
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Ints(prefixes)

	for _, p := range prefixes {
		got := append([]string(nil), byPrefix[p]...)
		sort.Strings(got)

		wanted, isAllowlisted := allowedPrefixCollisions[p]
		if isAllowlisted {
			wanted = append([]string(nil), wanted...)
			sort.Strings(wanted)
			if !slices.Equal(got, wanted) {
				return fmt.Errorf("migrate: prefix %d must contain exactly the allowlisted historical files %v, got %v — renaming, removing, or adding a file at an allowlisted prefix requires an explicit policy-change PR", p, wanted, got)
			}
			continue
		}

		if len(got) > 1 {
			return fmt.Errorf("migrate: prefix %d is reused by multiple migration files %v — each migration needs a unique numeric prefix; renumber the new file (only the documented historical collision may be allowlisted in code, and never for a new file)", p, got)
		}
	}

	return nil
}
