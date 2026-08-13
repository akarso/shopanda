package migrate

import "testing"

// TestCheckFilenameHygiene_RealMigrationsDir guards the actual migrations/ tree:
// it must pass with only the documented historical 025_* collision allowlisted.
func TestCheckFilenameHygiene_RealMigrationsDir(t *testing.T) {
	if err := CheckFilenameHygiene("../../../migrations"); err != nil {
		t.Fatalf("migrations/ failed filename hygiene check: %v", err)
	}
}

func TestCheckFilenameHygiene_AllowsHistoricalCollision(t *testing.T) {
	names := []string{
		"001_create_products.sql",
		"025_add_cart_merged_guest_id.sql",
		"025_create_invoices.sql",
		"026_create_url_rewrites.sql",
	}
	if err := checkFilenameHygiene(names); err != nil {
		t.Fatalf("expected the documented 025_* collision to pass, got: %v", err)
	}
}

func TestCheckFilenameHygiene_RejectsNewCollision(t *testing.T) {
	names := []string{
		"001_create_products.sql",
		"002_create_variants.sql",
		"002_create_something_else.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error for a new (non-allowlisted) prefix collision")
	}
}

func TestCheckFilenameHygiene_RejectsLeadingZeroNormalizedCollision(t *testing.T) {
	// "007" and "7" both normalize to the integer 7 and would sort adjacently
	// only by luck — this must be caught even though the raw strings differ.
	names := []string{
		"007_create_customers.sql",
		"7_create_customers_again.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error for a leading-zero-normalized prefix collision")
	}
}

func TestCheckFilenameHygiene_RejectsRenamedAllowlistEntry(t *testing.T) {
	names := []string{
		"025_add_cart_merged_guest_id.sql",
		"025_renamed_invoices.sql", // renamed from 025_create_invoices.sql
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error when an allowlisted historical filename is renamed")
	}
}

func TestCheckFilenameHygiene_RejectsRemovedAllowlistEntry(t *testing.T) {
	// Only one of the two allowlisted 025_* files present — no raw collision,
	// but it still violates the "no rename/removal without a policy PR" rule.
	names := []string{
		"025_add_cart_merged_guest_id.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error when an allowlisted historical filename is removed")
	}
}

func TestCheckFilenameHygiene_RejectsBothAllowlistEntriesRenamedAway(t *testing.T) {
	// Both historical files moved off prefix 25 — prefix 25 is absent from disk,
	// so a byPrefix-only check would miss this. Membership must still fail.
	names := []string{
		"001_create_products.sql",
		"090_add_cart_merged_guest_id_renamed.sql",
		"091_create_invoices_renamed.sql",
		"026_create_url_rewrites.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error when both allowlisted historical filenames are renamed away from prefix 25")
	}
}

func TestCheckFilenameHygiene_RejectsBothAllowlistEntriesDeleted(t *testing.T) {
	names := []string{
		"001_create_products.sql",
		"026_create_url_rewrites.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error when both allowlisted historical filenames are deleted")
	}
}

func TestCheckFilenameHygiene_RejectsExtraFileAtAllowlistedPrefix(t *testing.T) {
	names := []string{
		"025_add_cart_merged_guest_id.sql",
		"025_create_invoices.sql",
		"025_one_more.sql",
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error when a third file reuses the allowlisted prefix")
	}
}

func TestCheckFilenameHygiene_RejectsNonConformingFilename(t *testing.T) {
	names := []string{
		"001_create_products.sql",
		"add_missing_index.sql", // no numeric prefix
	}
	if err := checkFilenameHygiene(names); err == nil {
		t.Fatal("expected error for a .sql file without a numeric prefix")
	}
}

func TestCheckFilenameHygiene_IgnoresNonSQLFiles(t *testing.T) {
	names := []string{
		"001_create_products.sql",
		"025_add_cart_merged_guest_id.sql",
		"025_create_invoices.sql",
		"README.md",
	}
	if err := checkFilenameHygiene(names); err != nil {
		t.Fatalf("expected non-.sql files to be ignored, got: %v", err)
	}
}

func TestCheckFilenameHygiene_MissingDir(t *testing.T) {
	if err := CheckFilenameHygiene("/nonexistent/path"); err == nil {
		t.Fatal("expected error for a missing directory")
	}
}
