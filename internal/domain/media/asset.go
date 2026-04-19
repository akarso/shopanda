package media

import (
	"errors"
	"time"
)

// Asset represents a stored file.
type Asset struct {
	ID         string
	Path       string
	Filename   string
	MimeType   string
	Size       int64
	Meta       map[string]interface{}
	Thumbnails map[string]string // preset name → storage path
	CreatedAt  time.Time
}

// NewAsset creates a validated Asset.
func NewAsset(id, path, filename, mimeType string, size int64) (Asset, error) {
	if id == "" {
		return Asset{}, errors.New("asset id must not be empty")
	}
	if path == "" {
		return Asset{}, errors.New("asset path must not be empty")
	}
	if filename == "" {
		return Asset{}, errors.New("asset filename must not be empty")
	}
	if mimeType == "" {
		return Asset{}, errors.New("asset mime type must not be empty")
	}
	if size <= 0 {
		return Asset{}, errors.New("asset size must be positive")
	}
	return Asset{
		ID:         id,
		Path:       path,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       size,
		Meta:       make(map[string]interface{}),
		Thumbnails: make(map[string]string),
		CreatedAt:  time.Now().UTC(),
	}, nil
}
