package imaging

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"

	domainMedia "github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time check.
var _ domainMedia.ImageProcessor = (*Processor)(nil)

// Processor implements ImageProcessor using the disintegration/imaging library.
type Processor struct{}

// New returns a new Processor.
func New() *Processor { return &Processor{} }

// Resize decodes the image, resizes it, and re-encodes in the same format.
func (p *Processor) Resize(input io.Reader, opts domainMedia.ResizeOpts) (io.Reader, error) {
	img, err := imaging.Decode(input, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("imaging: decode: %w", err)
	}

	if !supportedMime(opts.MimeType) {
		return nil, fmt.Errorf("imaging: unsupported format: %s", opts.MimeType)
	}

	var resized image.Image
	switch opts.Fit {
	case "cover":
		resized = imaging.Fill(img, opts.Width, opts.Height, imaging.Center, imaging.Lanczos)
	case "contain":
		resized = imaging.Fit(img, opts.Width, opts.Height, imaging.Lanczos)
	case "fill", "":
		resized = imaging.Resize(img, opts.Width, opts.Height, imaging.Lanczos)
	default:
		return nil, fmt.Errorf("imaging: unsupported fit mode: %q", opts.Fit)
	}

	quality := opts.Quality
	if quality <= 0 {
		quality = 80
	}

	var buf bytes.Buffer
	if err := encodeImage(&buf, resized, opts.MimeType, quality); err != nil {
		return nil, err
	}

	return &buf, nil
}

// Format decodes the image from input and re-encodes it in the target MIME type.
func (p *Processor) Format(input io.Reader, mime string, quality int) (io.Reader, error) {
	img, err := imaging.Decode(input, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("imaging: decode for format: %w", err)
	}

	if quality <= 0 {
		quality = 80
	}

	var buf bytes.Buffer
	if err := encodeImage(&buf, img, mime, quality); err != nil {
		return nil, err
	}
	return &buf, nil
}

// encodeImage writes img to w in the format specified by mime.
func encodeImage(w *bytes.Buffer, img image.Image, mime string, quality int) error {
	switch mime {
	case "image/jpeg":
		return imaging.Encode(w, img, imaging.JPEG, imaging.JPEGQuality(quality))
	case "image/png":
		return imaging.Encode(w, img, imaging.PNG)
	case "image/gif":
		return imaging.Encode(w, img, imaging.GIF)
	case "image/webp":
		return webp.Encode(w, img, &webp.Options{Quality: float32(quality)})
	default:
		return fmt.Errorf("imaging: unsupported format: %s", mime)
	}
}

func supportedMime(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
