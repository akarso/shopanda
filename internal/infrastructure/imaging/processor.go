package imaging

import (
	"bytes"
	"fmt"
	"image"
	"io"

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

	format, ok := mimeToFormat(opts.MimeType)
	if !ok {
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
	var encOpts []imaging.EncodeOption
	if format == imaging.JPEG {
		encOpts = append(encOpts, imaging.JPEGQuality(quality))
	}
	if err := imaging.Encode(&buf, resized, format, encOpts...); err != nil {
		return nil, fmt.Errorf("imaging: encode: %w", err)
	}

	return &buf, nil
}

func mimeToFormat(mime string) (imaging.Format, bool) {
	switch mime {
	case "image/jpeg":
		return imaging.JPEG, true
	case "image/png":
		return imaging.PNG, true
	case "image/gif":
		return imaging.GIF, true
	default:
		return 0, false
	}
}
