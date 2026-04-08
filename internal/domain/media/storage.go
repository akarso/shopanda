package media

import "io"

// Storage abstracts file storage operations.
// Local filesystem is the core implementation; S3/CDN backends are provided via plugins.
type Storage interface {
	// Name returns the storage backend identifier (e.g. "local", "s3").
	Name() string

	// Save writes the contents of file to the given path.
	Save(path string, file io.Reader) error

	// Delete removes the file at the given path.
	Delete(path string) error

	// URL returns the public URL for the given storage-relative path.
	URL(path string) string
}
