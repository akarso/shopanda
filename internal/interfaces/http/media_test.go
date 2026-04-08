package http

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	mediaApp "github.com/akarso/shopanda/internal/application/media"
	domainMedia "github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- mocks (handler-level) ---

type hMockStorage struct{}

func (hMockStorage) Name() string                     { return "test" }
func (hMockStorage) Save(_ string, r io.Reader) error { io.Copy(io.Discard, r); return nil }
func (hMockStorage) Delete(_ string) error            { return nil }
func (hMockStorage) URL(path string) string           { return "/media/" + path }

type hMockAssetRepo struct{}

func (hMockAssetRepo) Save(_ context.Context, _ *domainMedia.Asset) error { return nil }
func (hMockAssetRepo) FindByID(_ context.Context, _ string) (*domainMedia.Asset, error) {
	return nil, nil
}

type hMockLogger struct{}

func (hMockLogger) Info(_ string, _ map[string]interface{})           {}
func (hMockLogger) Warn(_ string, _ map[string]interface{})           {}
func (hMockLogger) Error(_ string, _ error, _ map[string]interface{}) {}

func newTestMediaService() *mediaApp.Service {
	bus := event.NewBus(hMockLogger{})
	return mediaApp.NewService(hMockStorage{}, hMockAssetRepo{}, bus, hMockLogger{})
}

func TestMediaHandler_Upload(t *testing.T) {
	handler := NewMediaHandler(newTestMediaService())

	// Minimal JPEG header so http.DetectContentType returns image/jpeg.
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="photo.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(jpegHeader)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.Upload().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestMediaHandler_Upload_MissingFile(t *testing.T) {
	handler := NewMediaHandler(newTestMediaService())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", nil)
	rec := httptest.NewRecorder()

	handler.Upload().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestMediaHandler_Upload_TooLarge(t *testing.T) {
	handler := NewMediaHandler(newTestMediaService())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="big.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	// Write just over 10MB to exceed MaxBytesReader limit.
	part.Write(bytes.Repeat([]byte("x"), 10<<20+1))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.Upload().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
