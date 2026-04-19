package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
)

func makeJPEG(w, h int) io.Reader {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return &buf
}

func makePNG(w, h int) io.Reader {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return &buf
}

func TestResize_JPEG_Cover(t *testing.T) {
	p := New()
	result, err := p.Resize(makeJPEG(800, 600), domainMedia.ResizeOpts{
		Width:    150,
		Height:   150,
		Fit:      "cover",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, _, err := image.Decode(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 150 || bounds.Dy() != 150 {
		t.Errorf("size = %dx%d, want 150x150", bounds.Dx(), bounds.Dy())
	}
}

func TestResize_JPEG_Contain(t *testing.T) {
	p := New()
	result, err := p.Resize(makeJPEG(800, 400), domainMedia.ResizeOpts{
		Width:    400,
		Height:   400,
		Fit:      "contain",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, _, err := image.Decode(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 200 {
		t.Errorf("size = %dx%d, want 400x200", bounds.Dx(), bounds.Dy())
	}
}

func TestResize_PNG(t *testing.T) {
	p := New()
	result, err := p.Resize(makePNG(600, 600), domainMedia.ResizeOpts{
		Width:    150,
		Height:   150,
		Fit:      "cover",
		MimeType: "image/png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, format, err := image.Decode(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 150 || bounds.Dy() != 150 {
		t.Errorf("size = %dx%d, want 150x150", bounds.Dx(), bounds.Dy())
	}
}

func TestResize_Fill(t *testing.T) {
	p := New()
	result, err := p.Resize(makeJPEG(800, 400), domainMedia.ResizeOpts{
		Width:    200,
		Height:   100,
		Fit:      "fill",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	img, _, err := image.Decode(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 200 || bounds.Dy() != 100 {
		t.Errorf("size = %dx%d, want 200x100", bounds.Dx(), bounds.Dy())
	}
}

func TestResize_UnsupportedFormat(t *testing.T) {
	p := New()
	_, err := p.Resize(makeJPEG(100, 100), domainMedia.ResizeOpts{
		Width:    50,
		Height:   50,
		Fit:      "cover",
		MimeType: "image/webp",
	})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestResize_InvalidInput(t *testing.T) {
	p := New()
	_, err := p.Resize(bytes.NewReader([]byte("not an image")), domainMedia.ResizeOpts{
		Width:    50,
		Height:   50,
		Fit:      "cover",
		MimeType: "image/jpeg",
	})
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestResize_DefaultQuality(t *testing.T) {
	p := New()
	result, err := p.Resize(makeJPEG(200, 200), domainMedia.ResizeOpts{
		Width:    100,
		Height:   100,
		Fit:      "cover",
		Quality:  0,
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, format, err := image.Decode(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
}
