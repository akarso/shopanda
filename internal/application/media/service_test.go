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
	delErr  error
	saved   map[string]bool
	deleted []string
}

func (m *mockStorage) Name() string { return m.name }
func (m *mockStorage) Save(path string, r io.Reader) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.saved == nil {
		m.saved = make(map[string]bool)
	}
	// Drain the reader so countingReader records bytes.
	io.Copy(io.Discard, r)
	m.saved[path] = true
	return nil
}
func (m *mockStorage) Delete(path string) error {
	m.deleted = append(m.deleted, path)
	return m.delErr
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

type capturingLogger struct {
	errors []string
	warns  []string
}

func (c *capturingLogger) Info(_ string, _ map[string]interface{})   {}
func (c *capturingLogger) Warn(evt string, _ map[string]interface{}) { c.warns = append(c.warns, evt) }
func (c *capturingLogger) Error(evt string, _ error, _ map[string]interface{}) {
	c.errors = append(c.errors, evt)
}

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
	if result.Asset.Size != int64(len(jpegHeader)) {
		t.Errorf("size = %d, want %d", result.Asset.Size, len(jpegHeader))
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
		File:     bytes.NewReader(jpegHeader),
	})
	if err == nil {
		t.Fatal("expected error when persist fails")
	}
	if len(storage.deleted) != 1 {
		t.Errorf("should clean up stored file on persist failure, deleted = %d", len(storage.deleted))
	}
}

func TestUpload_PersistFails_DeleteAlsoFails(t *testing.T) {
	storage := &mockStorage{name: "test", delErr: errors.New("rm failed")}
	repo := &mockAssetRepo{saveErr: errors.New("db error")}
	log := &capturingLogger{}
	bus := event.NewBus(log)
	svc := NewService(storage, repo, bus, log)

	_, err := svc.Upload(context.Background(), UploadInput{
		Filename: "test.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err == nil {
		t.Fatal("expected error when persist fails")
	}
	if len(log.errors) != 1 {
		t.Fatalf("expected 1 logged error, got %d", len(log.errors))
	}
	if log.errors[0] != "media: rollback delete failed" {
		t.Errorf("logged error = %q, want %q", log.errors[0], "media: rollback delete failed")
	}
}

// --- mock processor ---

type mockProcessor struct {
	resizeErr   error
	webpErr     error // returned only when MimeType == "image/webp"
	formatErr   error
	called      int
	formatted   int
	resizeMimes []string
}

func (m *mockProcessor) Resize(input io.Reader, opts domainMedia.ResizeOpts) (io.Reader, error) {
	m.called++
	m.resizeMimes = append(m.resizeMimes, opts.MimeType)
	if opts.MimeType == "image/webp" && m.webpErr != nil {
		return nil, m.webpErr
	}
	if m.resizeErr != nil {
		return nil, m.resizeErr
	}
	// Drain input and return a tiny placeholder.
	io.Copy(io.Discard, input)
	return bytes.NewReader([]byte("thumb")), nil
}

func (m *mockProcessor) Format(input io.Reader, mime string, quality int) (io.Reader, error) {
	m.formatted++
	if m.formatErr != nil {
		return nil, m.formatErr
	}
	io.Copy(io.Discard, input)
	return bytes.NewReader([]byte("webp")), nil
}

// --- thumbnail tests ---

func TestUpload_WithThumbnails(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	proc := &mockProcessor{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})
	svc.SetImageProcessor(proc, []domainMedia.ThumbnailPreset{
		{Name: "small", Width: 150, Height: 150, Fit: "cover"},
		{Name: "large", Width: 800, Height: 800, Fit: "contain"},
	})

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.called != 2 {
		t.Errorf("processor called %d times, want 2", proc.called)
	}
	// 1 original + 2 thumbnails = 3 saved files.
	if len(storage.saved) != 3 {
		t.Errorf("saved files = %d, want 3", len(storage.saved))
	}
	if len(result.Thumbnails) != 2 {
		t.Errorf("thumbnail URLs = %d, want 2", len(result.Thumbnails))
	}
	if result.Thumbnails["small"] == "" {
		t.Error("missing thumbnail URL for 'small'")
	}
	if result.Thumbnails["large"] == "" {
		t.Error("missing thumbnail URL for 'large'")
	}
	// Asset should contain thumbnail paths (not URLs).
	if len(result.Asset.Thumbnails) != 2 {
		t.Errorf("asset thumbnails = %d, want 2", len(result.Asset.Thumbnails))
	}
}

func TestUpload_ThumbnailResizeFails(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	proc := &mockProcessor{resizeErr: errors.New("decode error")}
	log := &capturingLogger{}
	bus := event.NewBus(log)
	svc := NewService(storage, repo, bus, log)
	svc.SetImageProcessor(proc, []domainMedia.ThumbnailPreset{
		{Name: "small", Width: 150, Height: 150, Fit: "cover"},
	})

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Upload succeeds even if thumbnails fail.
	if result.Asset.ID == "" {
		t.Error("asset ID is empty")
	}
	// Only 1 saved file (original); thumbnail was not saved.
	if len(storage.saved) != 1 {
		t.Errorf("saved files = %d, want 1", len(storage.saved))
	}
	if len(result.Thumbnails) != 0 {
		t.Errorf("thumbnail URLs = %d, want 0", len(result.Thumbnails))
	}
	if len(log.warns) == 0 {
		t.Error("expected warning log for failed thumbnail")
	}
}

func TestUpload_NoProcessor(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})
	// No SetImageProcessor call.

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Thumbnails) != 0 {
		t.Errorf("thumbnail URLs = %d, want 0 (no processor)", len(result.Thumbnails))
	}
	// Only original saved.
	if len(storage.saved) != 1 {
		t.Errorf("saved files = %d, want 1", len(storage.saved))
	}
}

func TestUpload_WithWebP(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	proc := &mockProcessor{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})
	svc.SetImageProcessor(proc, []domainMedia.ThumbnailPreset{
		{Name: "small", Width: 150, Height: 150, Fit: "cover"},
	})
	svc.SetWebPConfig(true, 80)

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 original + 1 thumbnail + 1 WebP = 3 saved.
	if len(storage.saved) != 3 {
		t.Errorf("saved files = %d, want 3", len(storage.saved))
	}
	// Resize called twice: once for JPEG thumbnail, once for WebP variant.
	if proc.called != 2 {
		t.Errorf("resize called %d times, want 2", proc.called)
	}
	if proc.formatted != 0 {
		t.Errorf("format called %d times, want 0", proc.formatted)
	}
	// Verify the second Resize targeted WebP.
	if len(proc.resizeMimes) != 2 || proc.resizeMimes[1] != "image/webp" {
		t.Errorf("resize mimes = %v, want [..., image/webp]", proc.resizeMimes)
	}
	if result.Thumbnails["small"] == "" {
		t.Error("missing thumbnail URL for 'small'")
	}
	if result.Thumbnails["small_webp"] == "" {
		t.Error("missing thumbnail URL for 'small_webp'")
	}
	if len(result.Thumbnails) != 2 {
		t.Errorf("thumbnail URLs = %d, want 2", len(result.Thumbnails))
	}
}

func TestUpload_WebPConversionFails(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	proc := &mockProcessor{webpErr: errors.New("webp encode failed")}
	log := &capturingLogger{}
	bus := event.NewBus(log)
	svc := NewService(storage, repo, bus, log)
	svc.SetImageProcessor(proc, []domainMedia.ThumbnailPreset{
		{Name: "small", Width: 150, Height: 150, Fit: "cover"},
	})
	svc.SetWebPConfig(true, 80)

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Upload succeeds; original + JPEG thumbnail saved, WebP skipped.
	if len(storage.saved) != 2 {
		t.Errorf("saved files = %d, want 2", len(storage.saved))
	}
	// Only JPEG thumbnail, no WebP.
	if len(result.Thumbnails) != 1 {
		t.Errorf("thumbnail URLs = %d, want 1", len(result.Thumbnails))
	}
	if result.Thumbnails["small"] == "" {
		t.Error("missing thumbnail URL for 'small'")
	}
	if len(log.warns) == 0 {
		t.Error("expected warning log for failed WebP conversion")
	}
}

func TestUpload_WebPDisabled(t *testing.T) {
	storage := &mockStorage{name: "test"}
	repo := &mockAssetRepo{}
	proc := &mockProcessor{}
	bus := event.NewBus(mockLogger{})
	svc := NewService(storage, repo, bus, mockLogger{})
	svc.SetImageProcessor(proc, []domainMedia.ThumbnailPreset{
		{Name: "small", Width: 150, Height: 150, Fit: "cover"},
	})
	// WebP not enabled — no SetWebPConfig call.

	result, err := svc.Upload(context.Background(), UploadInput{
		Filename: "photo.jpg",
		File:     bytes.NewReader(jpegHeader),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 original + 1 thumbnail, no WebP.
	if len(storage.saved) != 2 {
		t.Errorf("saved files = %d, want 2", len(storage.saved))
	}
	if proc.called != 1 {
		t.Errorf("resize called %d times, want 1", proc.called)
	}
	if proc.formatted != 0 {
		t.Errorf("format called %d times, want 0", proc.formatted)
	}
	if len(result.Thumbnails) != 1 {
		t.Errorf("thumbnail URLs = %d, want 1", len(result.Thumbnails))
	}
}
