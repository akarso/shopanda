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

func TestIsPrivateDownloadsPath(t *testing.T) {
	if !media.IsPrivateDownloadsPath("private/downloads/x/y.pdf") {
		t.Fatal("expected private path")
	}
	if !media.IsPrivateDownloadsPath("/private/downloads/x/y.pdf") {
		t.Fatal("expected leading-slash private path")
	}
	if media.IsPrivateDownloadsPath("uploads/x/y.pdf") {
		t.Fatal("uploads must not be private")
	}
}

func TestSanitizeContentDispositionFilename(t *testing.T) {
	if got := media.SanitizeContentDispositionFilename("bad\"\r\nfile.bin"); got != "badfile.bin" {
		t.Fatalf("got %q", got)
	}
	if got := media.SanitizeContentDispositionFilename("   "); got != "download" {
		t.Fatalf("empty = %q", got)
	}
	if got := media.SanitizeContentDispositionFilename("café.pdf"); got != "caf.pdf" {
		t.Fatalf("non-ASCII ascii fallback = %q", got)
	}
}

func TestContentDispositionAttachment(t *testing.T) {
	if got := media.ContentDispositionAttachment("guide.pdf"); got != `attachment; filename="guide.pdf"` {
		t.Fatalf("ASCII-only = %q", got)
	}
	if got := media.ContentDispositionAttachment("bad\"\r\nfile.bin"); got != `attachment; filename="badfile.bin"` {
		t.Fatalf("unsafe ASCII = %q", got)
	}
	got := media.ContentDispositionAttachment("café.pdf")
	want := `attachment; filename="caf.pdf"; filename*=UTF-8''caf%C3%A9.pdf`
	if got != want {
		t.Fatalf("non-ASCII = %q, want %q", got, want)
	}
	got = media.ContentDispositionAttachment("文件")
	want = `attachment; filename="download"; filename*=UTF-8''%E6%96%87%E4%BB%B6`
	if got != want {
		t.Fatalf("all non-ASCII = %q, want %q", got, want)
	}
	if got := media.ContentDispositionAttachment("   "); got != `attachment; filename="download"` {
		t.Fatalf("empty = %q", got)
	}
}
