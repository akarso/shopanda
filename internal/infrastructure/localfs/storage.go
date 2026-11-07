package localfs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
)

// Compile-time checks.
var (
	_ media.Storage          = (*Storage)(nil)
	_ media.ReadableStorage  = (*Storage)(nil)
	_ media.SignedURLStorage = (*Storage)(nil)
)

// SignedPathPrefix is the HTTP path prefix for HMAC local signed GETs.
// Relative SignedURL values use this prefix (no host) so redirects stay on-app.
const SignedPathPrefix = "/api/v1/media/local-signed/"

// Storage implements media.Storage using the local filesystem and
// SignedURLStorage via an HMAC token served by Handler.
type Storage struct {
	basePath string
	baseURL  string
	secret   []byte
}

// New creates a local filesystem Storage. A process-ephemeral signing secret
// is generated when secret is nil/empty (URLs invalidate on restart).
func New(basePath, baseURL string) *Storage {
	return NewWithSecret(basePath, baseURL, nil)
}

// NewWithSecret creates Storage with an explicit HMAC secret for SignedURL.
func NewWithSecret(basePath, baseURL string, secret []byte) *Storage {
	if basePath == "" {
		panic("localfs.New: empty basePath")
	}
	if baseURL == "" {
		panic("localfs.New: empty baseURL")
	}
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			panic("localfs.New: generate sign secret: " + err.Error())
		}
	}
	return &Storage{
		basePath: basePath,
		baseURL:  baseURL,
		secret:   append([]byte(nil), secret...),
	}
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
	return abs, nil
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

// Head verifies the object exists (SignedURLStorage).
func (s *Storage) Head(_ context.Context, path string) error {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}
	_, err = os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("localfs: head %q: %w", path, os.ErrNotExist)
		}
		return fmt.Errorf("localfs: head %q: %w", path, err)
	}
	return nil
}

// SignedURL returns a relative HMAC URL under SignedPathPrefix.
// Callers must not log the URL. Prefer this over permanent URL() for private blobs.
func (s *Storage) SignedURL(_ context.Context, path string, opts media.SignURLOptions) (string, error) {
	if _, err := s.resolveSafePath(path); err != nil {
		return "", err
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	token, err := s.sign(path, opts.Filename, exp)
	if err != nil {
		return "", err
	}
	return SignedPathPrefix + token, nil
}

// SignedURLHosts returns nil — local signed URLs are relative (same origin).
func (s *Storage) SignedURLHosts() []string { return nil }

func (s *Storage) sign(path, filename string, exp int64) (string, error) {
	// payload: exp(8) | pathLen(2) | path | filename
	if len(path) > 65535 {
		return "", fmt.Errorf("localfs: path too long")
	}
	payload := make([]byte, 8+2+len(path)+len(filename))
	binary.BigEndian.PutUint64(payload[:8], uint64(exp))
	binary.BigEndian.PutUint16(payload[8:10], uint16(len(path)))
	copy(payload[10:], path)
	copy(payload[10+len(path):], filename)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	raw := append(payload, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ParseToken validates token and returns storage path + optional filename.
func (s *Storage) ParseToken(token string) (path, filename string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", fmt.Errorf("localfs: token decode: %w", err)
	}
	const macLen = sha256.Size
	if len(raw) < 8+2+macLen {
		return "", "", fmt.Errorf("localfs: token too short")
	}
	payload := raw[:len(raw)-macLen]
	sum := raw[len(raw)-macLen:]
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return "", "", fmt.Errorf("localfs: bad mac")
	}
	exp := int64(binary.BigEndian.Uint64(payload[:8]))
	if time.Now().UTC().Unix() > exp {
		return "", "", fmt.Errorf("localfs: token expired")
	}
	pathLen := int(binary.BigEndian.Uint16(payload[8:10]))
	if 10+pathLen > len(payload) {
		return "", "", fmt.Errorf("localfs: bad path length")
	}
	path = string(payload[10 : 10+pathLen])
	filename = string(payload[10+pathLen:])
	if _, err := s.resolveSafePath(path); err != nil {
		return "", "", err
	}
	return path, filename, nil
}

// ExpiredTokenForTest builds an already-expired token (tests).
func (s *Storage) ExpiredTokenForTest(path, filename string) (string, error) {
	if _, err := s.resolveSafePath(path); err != nil {
		return "", err
	}
	return s.sign(path, filename, time.Now().UTC().Add(-time.Minute).Unix())
}
