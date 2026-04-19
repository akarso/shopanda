package media

import "io"

// ResizeOpts controls how an image is resized.
type ResizeOpts struct {
	Width    int
	Height   int
	Fit      string // "cover", "contain", "fill"
	Quality  int    // 1-100, 0 = default (80)
	MimeType string // output format: image/jpeg, image/png, etc.
}

// ThumbnailPreset defines a named thumbnail configuration.
type ThumbnailPreset struct {
	Name   string
	Width  int
	Height int
	Fit    string // "cover", "contain", "fill"
}

// ImageProcessor resizes and converts images.
type ImageProcessor interface {
	// Resize decodes the image from input, resizes it according to opts,
	// and returns the re-encoded result.
	Resize(input io.Reader, opts ResizeOpts) (io.Reader, error)

	// Format decodes the image from input and re-encodes it in the given
	// MIME type (e.g. "image/webp") at the specified quality (1-100, 0 = default).
	Format(input io.Reader, mime string, quality int) (io.Reader, error)
}
