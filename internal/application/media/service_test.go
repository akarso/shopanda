package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- mocks ---

type mockStorage struct {
	name    string
	saveErr error
	saved   map[string]bool
	deleted []string
}

func (m *mockStorage) Name() string { return m.name }
func (m *mockStorage) Save(path string, _ io.Reader) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.saved == nil {
		m.saved = make(map[string]bool)
	}
	m.saved[path] = true
	return nil
}
func (m *mockStorage) Delete(path string) error {
	m.deleted = append(m.deleted, path)
	return nil
}
func (m *mockStorage) URL(path string) string { return "/media/" + path }

type mockAssetRepo struct {
	saveErr error
	saved   []*domainMedia.Asset
}

func (m *mockAssetRepo) Save(_ context.Context, a *domainMedia.Asset) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, a)
	return nil
}

func (m *mockAssetRepo) FindByID(_ context.Context, _ string) (*domainMedia.Asset, error) {
	return nil, nil
}

type mockLogger struct{}

func (mockLogger) Info(_ string, _ map[string]interface{})           {}
func (mockLogger) Warn(_ string, _ map[string]interface{})           {}
func (mockLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// --- tests ---

// jpegHeader is a minimal JPEG file header (SOI + APP0 marker).
var jpegHeader = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}

func TestUpload(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "test.jpg",
		MimeType: "image/jpeg",
		Size:     1024,
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Asset.Filename != "test.jpg" {
		t.Errorf("filename = %q, want %q", result.Asset.Filename, "test.jpg")
	}
	if result.Asset.MimeType != "image/jpeg" {
		t.Errorf("mime_type = %q, want %q", result.Asset.MimeType, "image/jpeg")
	}
	if result.Asset.Size != 1024 {
		t.Errorf("size = %d, want 1024", result.Asset.Size)
	}
	if result.URL == "" {
		t.Error("url is empty")
	}
	if len(storage.saved) != 1 {
		t.Errorf("saved files = %d, want 1", len(storage.saved))
	}
	if len(repo.saved) != 1 {
		t.Errorf("saved assets = %d, want 1", len(repo.saved))
	}
}

func TestUpload_UnsupportedMimeType(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})

	_, err := svc.Upload(context.Background(), UploadInput{
		Filename: "test.exe",
		MimeType: "application/octet-stream",
		Size:     1024,
		File:     bytes.NewReader([]byte("plain text content, not an image")),
	})
	if err == nil {
		t.Fatal("expected error for unsupported mime type")
	}
	if len(storage.saved) != 0 {
		t.Error("file should not be saved for unsupported type")
	}
}

func TestUpload_StorageSaveFails(t *testing.T) {
	storage := &mockStorage{name: "test", saveErr: errors.New("disk full")}
	repo := &mockAssetRepo{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})

	_, err := svc.Upload(context.Background(), UploadInput{
		Filename: "test.jpg",
		MimeType: "image/jpeg",
		Size:     1024,
		File:     bytes.NewReader(jpegHeader),
	})
	if err == nil {
		t.Fatal("expected error when storage fails")
	}
	if len(repo.saved) != 0 {
		t.Error("asset should not be persisted when storage fails")
	}
}

func TestUpload_PersistFails(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{saveErr: errors.New("db error")}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})

	_, err := svc.Upload(context.Background(), UploadInput{
		Filename: "test.jpg",
		MimeType: "image/jpeg",
		Size:     1024,
		File:     bytes.NewReader(jpegHeader),
	})
	if err == nil {
		t.Fatal("expected error when persist fails")
	}
	if len(storage.deleted) != 1 {
		t.Errorf("should clean up stored file on persist failure, deleted = %d", len(storage.deleted))
	}
}
