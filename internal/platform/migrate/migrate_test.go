package migrate

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestListMigrations_SortsFiles(t *testing.T) {
	dir := t.TempDir()

	names := []string{"003_third.sql", "001_first.sql", "002_second.sql"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("SELECT 1;"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Add a non-sql file that should be ignored.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0644)

	files, err := listMigrations(dir)
	if err != nil {
		t.Fatalf("listMigrations: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	// Verify sorted order.
	basenames := make([]string, len(files))
	for i, f := range files {
		basenames[i] = filepath.Base(f)
	}
	if !sort.StringsAreSorted(basenames) {
		t.Fatalf("files not sorted: %v", basenames)
	}

	expected := []string{"001_first.sql", "002_second.sql", "003_third.sql"}
	for i, name := range expected {
		if basenames[i] != name {
			t.Errorf("index %d: expected %s, got %s", i, name, basenames[i])
		}
	}
}

func TestListMigrations_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	files, err := listMigrations(dir)
	if err != nil {
		t.Fatalf("listMigrations: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestListMigrations_MissingDir(t *testing.T) {
	_, err := listMigrations("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}
