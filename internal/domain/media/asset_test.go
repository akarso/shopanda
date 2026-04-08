package media_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
)

func TestNewAsset(t *testing.T) {
	a, err := media.NewAsset("a1", "products/img.jpg", "img.jpg", "image/jpeg", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if a.ID != "a1" {
		t.Errorf("ID = %q", a.ID)
	}
	if a.Path != "products/img.jpg" {
		t.Errorf("Path = %q", a.Path)
	}
	if a.Filename != "img.jpg" {
		t.Errorf("Filename = %q", a.Filename)
	}
	if a.MimeType != "image/jpeg" {
		t.Errorf("MimeType = %q", a.MimeType)
	}
	if a.Size != 1024 {
		t.Errorf("Size = %d", a.Size)
	}
	if a.Meta == nil {
		t.Error("Meta is nil")
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestNewAsset_Validation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		path     string
		filename string
		mime     string
		size     int64
	}{
		{"empty id", "", "p", "f", "m", 1},
		{"empty path", "1", "", "f", "m", 1},
		{"empty filename", "1", "p", "", "m", 1},
		{"empty mime", "1", "p", "f", "", 1},
		{"zero size", "1", "p", "f", "m", 0},
		{"negative size", "1", "p", "f", "m", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := media.NewAsset(tt.id, tt.path, tt.filename, tt.mime, tt.size)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
