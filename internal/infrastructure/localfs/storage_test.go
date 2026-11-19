package localfs_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
)

func mustLocal(t *testing.T, basePath, baseURL string) *localfs.Storage {
	t.Helper()
	s, err := localfs.NewWithSecret(basePath, baseURL, []byte("unit-test-sign-secret-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Compile-time interface checks.
var (
	_ media.Storage          = (*localfs.Storage)(nil)
	_ media.ReadableStorage  = (*localfs.Storage)(nil)
	_ media.SignedURLStorage = (*localfs.Storage)(nil)
)

func TestStorage_Name(t *testing.T) {
	s := mustLocal(t, t.TempDir(), "/media")
	if s.Name() != "local" {
		t.Errorf("Name() = %q, want %q", s.Name(), "local")
	}
}

func TestStorage_SaveAndURL(t *testing.T) {
	dir := t.TempDir()
	s := mustLocal(t, dir, "/media")

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
	s := mustLocal(t, dir, "/media")

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
	s := mustLocal(t, t.TempDir(), "/media")
	// Deleting a non-existent file should not error.
	if err := s.Delete("no-such-file.txt"); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestStorage_Save_DirectoryTraversal(t *testing.T) {
	dir := t.TempDir()
	s := mustLocal(t, dir, "/media")

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
	s := mustLocal(t, dir, "/media")

	err := s.Delete("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for directory traversal")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("error = %q, want mention of escapes base directory", err.Error())
	}
}

func TestStorage_Read(t *testing.T) {
	dir := t.TempDir()
	s := mustLocal(t, dir, "/media")

	content := []byte("%PDF-from-disk")
	if err := s.Save("invoices/inv-1/invoice-1.pdf", strings.NewReader(string(content))); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Read(context.Background(), "invoices/inv-1/invoice-1.pdf")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("Read = %q, want %q", got, content)
	}
}

func TestStorage_Read_DirectoryTraversal(t *testing.T) {
	s := mustLocal(t, t.TempDir(), "/media")

	_, err := s.Read(context.Background(), "../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for directory traversal")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Errorf("error = %q, want mention of escapes base directory", err.Error())
	}
}

func TestStorage_ReadContext_Canceled(t *testing.T) {
	dir := t.TempDir()
	s := mustLocal(t, dir, "/media")

	// Multi-chunk blob so a non-cancelled read would loop; cancelled ctx
	// must return before loading the full object into memory.
	const size = 128 * 1024
	content := bytes.Repeat([]byte("x"), size)
	if err := s.Save("blobs/large.bin", bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := s.ReadContext(ctx, "blobs/large.bin")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("got %d bytes, want nil on cancel", len(got))
	}
}

func TestStorage_ReadContext_MultiChunk(t *testing.T) {
	dir := t.TempDir()
	s := mustLocal(t, dir, "/media")

	content := bytes.Repeat([]byte("ab"), 40*1024) // 80 KiB > one 32 KiB chunk
	if err := s.Save("blobs/multi.bin", bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.ReadContext(context.Background(), "blobs/multi.bin")
	if err != nil {
		t.Fatalf("ReadContext: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("len=%d, want %d", len(got), len(content))
	}
}
