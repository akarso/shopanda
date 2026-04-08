package localfs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
)

// Compile-time interface check.
var _ media.Storage = (*localfs.Storage)(nil)

func TestStorage_Name(t *testing.T) {
	s := localfs.New(t.TempDir(), "/media")
	if s.Name() != "local" {
		t.Errorf("Name() = %q, want %q", s.Name(), "local")
	}
}

func TestStorage_SaveAndURL(t *testing.T) {
	dir := t.TempDir()
	s := localfs.New(dir, "/media")

	content := "hello world"
	if err := s.Save("products/test.txt", strings.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists on disk.
	data, err := os.ReadFile(filepath.Join(dir, "products", "test.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}

	// Verify URL.
	url := s.URL("products/test.txt")
	if url != "/media/products/test.txt" {
		t.Errorf("URL = %q, want %q", url, "/media/products/test.txt")
	}
}

func TestStorage_Delete(t *testing.T) {
	dir := t.TempDir()
	s := localfs.New(dir, "/media")

	// Save then delete.
	if err := s.Save("delete-me.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("delete-me.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify file removed.
	_, err := os.Stat(filepath.Join(dir, "delete-me.txt"))
	if !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestStorage_Delete_NonExistent(t *testing.T) {
	s := localfs.New(t.TempDir(), "/media")
	// Deleting a non-existent file should not error.
	if err := s.Delete("no-such-file.txt"); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestStorage_Save_DirectoryTraversal(t *testing.T) {
	dir := t.TempDir()
	s := localfs.New(dir, "/media")

	err := s.Save("../../etc/passwd", strings.NewReader("pwned"))
	if err == nil {
		t.Fatal("expected error for directory traversal")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("error = %q, want mention of escapes base directory", err.Error())
	}
}

func TestStorage_Delete_DirectoryTraversal(t *testing.T) {
	dir := t.TempDir()
	s := localfs.New(dir, "/media")

	err := s.Delete("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for directory traversal")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("error = %q, want mention of escapes base directory", err.Error())
	}
}
