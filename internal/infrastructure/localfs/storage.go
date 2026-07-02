package localfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time checks.
var (
	_ media.Storage         = (*Storage)(nil)
	_ media.ReadableStorage = (*Storage)(nil)
)

// Storage implements media.Storage using the local filesystem.
type Storage struct {
	basePath string
	baseURL  string
}

// New creates a local filesystem Storage.
// basePath is the directory root (e.g. "./public/media").
// baseURL is the URL prefix (e.g. "/media").
func New(basePath, baseURL string) *Storage {
	if basePath == "" {
		panic("localfs.New: empty basePath")
	}
	if baseURL == "" {
		panic("localfs.New: empty baseURL")
	}
	return &Storage{basePath: basePath, baseURL: baseURL}
}

// Name returns "local".
func (s *Storage) Name() string { return "local" }

func (s *Storage) resolveSafePath(path string) (string, error) {
	full := filepath.Join(s.basePath, filepath.Clean(path))

	abs, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("localfs: resolve path: %w", err)
	}
	base, err := filepath.Abs(s.basePath)
	if err != nil {
		return "", fmt.Errorf("localfs: resolve base: %w", err)
	}
	if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("localfs: path escapes base directory")
	}
	return full, nil
}

// Save writes file contents to basePath/path.
// Intermediate directories are created as needed.
func (s *Storage) Save(path string, file io.Reader) error {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("localfs: mkdir: %w", err)
	}

	f, err := os.Create(full)
	if err != nil {
		return fmt.Errorf("localfs: create file: %w", err)
	}

	if _, err := io.Copy(f, file); err != nil {
		f.Close()
		os.Remove(full)
		return fmt.Errorf("localfs: write file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("localfs: close file: %w", err)
	}

	return nil
}

// Delete removes the file at basePath/path.
func (s *Storage) Delete(path string) error {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}

	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("localfs: delete: %w", err)
	}
	return nil
}

// Read returns file contents from basePath/path.
func (s *Storage) Read(path string) ([]byte, error) {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("localfs: read: %w", err)
	}
	return data, nil
}

// URL returns baseURL/path. Returns empty string if path escapes the storage root.
func (s *Storage) URL(path string) string {
	cleaned := filepath.Clean(path)
	rel, err := filepath.Rel(".", cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return s.baseURL + "/" + filepath.ToSlash(rel)
}
