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
	"unicode/utf8"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/config"
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

// MaxSignedTokenBytes caps inbound token size before base64/HMAC work (DoS).
const MaxSignedTokenBytes = 4096

// maxSignedPathBytes / maxSignedFilenameRunes bound token payload size.
const (
	maxSignedPathBytes     = 1024
	maxSignedFilenameRunes = 255
	signedExpirySkew       = 30 * time.Second
)

// Storage implements media.Storage using the local filesystem and
// SignedURLStorage via an HMAC token served by Handler.
type Storage struct {
	basePath string
	baseURL  string
	secret   []byte
}

// NewWithSecret creates Storage with an explicit HMAC secret for SignedURL.
// secret must be at least config.MinLocalSignSecretBytes long.
func NewWithSecret(basePath, baseURL string, secret []byte) (*Storage, error) {
	if basePath == "" {
		return nil, fmt.Errorf("localfs: empty basePath")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("localfs: empty baseURL")
	}
	if len(secret) < config.MinLocalSignSecretBytes {
		return nil, fmt.Errorf("localfs: sign secret must be at least %d bytes", config.MinLocalSignSecretBytes)
	}
	return &Storage{
		basePath: basePath,
		baseURL:  baseURL,
		secret:   append([]byte(nil), secret...),
	}, nil
}

// NewEphemeral is for SHOPANDA_DEV_MODE only when sign_secret is unset.
// URLs invalidate on process restart and across replicas.
func NewEphemeral(basePath, baseURL string) (*Storage, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("localfs: generate sign secret: %w", err)
	}
	return NewWithSecret(basePath, baseURL, secret)
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
	if realBase, err := filepath.EvalSymlinks(base); err == nil {
		base = realBase
	}
	// Containment against the real base. For missing targets (Save), resolve
	// the parent and re-join the final name so a symlink parent cannot escape.
	check := abs
	if _, err := os.Lstat(abs); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("localfs: resolve path: %w", err)
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("localfs: resolve parent: %w", err)
			}
			// Parent missing: ensure cleaned abs still under base via prefix.
			parent = filepath.Dir(abs)
		}
		check = filepath.Join(parent, filepath.Base(abs))
	} else {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			check = real
		}
	}
	if check != base && !strings.HasPrefix(check, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("localfs: path escapes base directory")
	}
	return abs, nil
}

// Save writes file contents to basePath/path.
func (s *Storage) Save(path string, file io.Reader) error {
	return s.SaveWithContentType(path, file, "")
}

// SaveWithContentType writes file contents. contentType is ignored locally
// (MIME comes from ServeContent / filename on signed GET); private keys use
// restrictive directory/file modes (including chmod of existing ancestors).
func (s *Storage) SaveWithContentType(path string, file io.Reader, _ string) error {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}

	dirMode := os.FileMode(0o755)
	fileMode := os.FileMode(0o644)
	private := media.IsPrivatePath(path)
	if private {
		dirMode = 0o700
		fileMode = 0o600
	}

	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("localfs: mkdir: %w", err)
	}
	if private {
		if err := chmodAncestors(dir, s.basePath, 0o700); err != nil {
			return fmt.Errorf("localfs: chmod private dirs: %w", err)
		}
	}

	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
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
	_ = os.Chmod(full, fileMode)

	return nil
}

// chmodAncestors walks from dir up to basePath (inclusive of dir, exclusive of
// parent-of-base) and applies mode. Used so private writes under a legacy
// 0755 tree still tighten permissions after upgrade.
func chmodAncestors(dir, basePath string, mode os.FileMode) error {
	baseAbs, err := filepath.Abs(basePath)
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(baseAbs); err == nil {
		baseAbs = real
	}
	cur, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for {
		if err := os.Chmod(cur, mode); err != nil && !os.IsNotExist(err) {
			return err
		}
		if cur == baseAbs {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur || !strings.HasPrefix(cur, baseAbs+string(os.PathSeparator)) && cur != baseAbs {
			break
		}
		cur = parent
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

// Open returns a read-only file for streaming signed GETs.
func (s *Storage) Open(path string) (*os.File, error) {
	full, err := s.resolveSafePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("localfs: open: %w", err)
	}
	return f, nil
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
func (s *Storage) SignedURL(_ context.Context, path string, opts media.SignURLOptions) (string, error) {
	if _, err := s.resolveSafePath(path); err != nil {
		return "", err
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	token, err := s.sign(path, opts.Filename, exp, media.NormalizeDisposition(opts.Disposition))
	if err != nil {
		return "", err
	}
	return SignedPathPrefix + token, nil
}

// SignedURLHosts returns nil — local signed URLs are relative (same origin).
func (s *Storage) SignedURLHosts() []string { return nil }

// signedTokenVersionV1 prefixes the HMAC payload. v0 (no version byte) remains
// parseable for rolling deploys; unix exp's high byte is 0 for decades.
const signedTokenVersionV1 byte = 1

func (s *Storage) sign(path, filename string, exp int64, disp media.Disposition) (string, error) {
	if len(path) > maxSignedPathBytes {
		return "", fmt.Errorf("localfs: path too long")
	}
	filename = truncateFilenameForSign(filename)
	dispByte := byte(0)
	if disp == media.DispositionInline {
		dispByte = 1
	}
	// v1: version(1) + exp(8) + pathLen(2) + path + filename + disp(1)
	payload := make([]byte, 1+8+2+len(path)+len(filename)+1)
	payload[0] = signedTokenVersionV1
	binary.BigEndian.PutUint64(payload[1:9], uint64(exp))
	binary.BigEndian.PutUint16(payload[9:11], uint16(len(path)))
	copy(payload[11:], path)
	copy(payload[11+len(path):], filename)
	payload[len(payload)-1] = dispByte
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	raw := append(payload, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// signV0ForTest builds a pre-versioning token (exp+pathLen+path+filename+mac).
// Used only by compat tests in this package.
func (s *Storage) signV0ForTest(path, filename string, exp int64) (string, error) {
	filename = truncateFilenameForSign(filename)
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

// ParseToken validates token and returns storage path, filename, and disposition.
// Accepts v1 (versioned + disposition) and legacy v0 (attachment, full filename).
func (s *Storage) ParseToken(token string) (path, filename string, disp media.Disposition, err error) {
	if len(token) > MaxSignedTokenBytes {
		return "", "", media.DispositionAttachment, fmt.Errorf("localfs: token too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", media.DispositionAttachment, fmt.Errorf("localfs: token decode: %w", err)
	}
	const macLen = sha256.Size
	if len(raw) < 8+2+macLen {
		return "", "", media.DispositionAttachment, fmt.Errorf("localfs: token too short")
	}
	payload := raw[:len(raw)-macLen]
	sum := raw[len(raw)-macLen:]
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return "", "", media.DispositionAttachment, fmt.Errorf("localfs: bad mac")
	}

	var pathLen int
	var rest []byte
	disp = media.DispositionAttachment
	if len(payload) >= 1+8+2+1 && payload[0] == signedTokenVersionV1 {
		exp := int64(binary.BigEndian.Uint64(payload[1:9]))
		if err := checkExpiry(exp); err != nil {
			return "", "", media.DispositionAttachment, err
		}
		pathLen = int(binary.BigEndian.Uint16(payload[9:11]))
		if 11+pathLen >= len(payload) {
			return "", "", media.DispositionAttachment, fmt.Errorf("localfs: bad path length")
		}
		path = string(payload[11 : 11+pathLen])
		rest = payload[11+pathLen:]
		if len(rest) < 1 {
			return "", "", media.DispositionAttachment, fmt.Errorf("localfs: missing disposition")
		}
		filename = string(rest[:len(rest)-1])
		if rest[len(rest)-1] == 1 {
			disp = media.DispositionInline
		}
	} else {
		// Legacy v0: exp(8)+pathLen(2)+path+filename (attachment).
		exp := int64(binary.BigEndian.Uint64(payload[:8]))
		if err := checkExpiry(exp); err != nil {
			return "", "", media.DispositionAttachment, err
		}
		pathLen = int(binary.BigEndian.Uint16(payload[8:10]))
		if 10+pathLen > len(payload) {
			return "", "", media.DispositionAttachment, fmt.Errorf("localfs: bad path length")
		}
		path = string(payload[10 : 10+pathLen])
		filename = string(payload[10+pathLen:])
	}
	if strings.Contains(path, "..") || strings.Contains(path, `\`) {
		return "", "", media.DispositionAttachment, fmt.Errorf("localfs: invalid path in token")
	}
	if _, err := s.resolveSafePath(path); err != nil {
		return "", "", media.DispositionAttachment, err
	}
	return path, filename, disp, nil
}

func checkExpiry(exp int64) error {
	now := time.Now().UTC().Unix()
	if now > exp+int64(signedExpirySkew.Seconds()) {
		return fmt.Errorf("localfs: token expired")
	}
	return nil
}

// truncateFilenameForSign hard-caps by Unicode runes so the token payload
// never splits a multi-byte character. Disposition sanitization stays in the
// HTTP handler (ContentDispositionAttachment).
func truncateFilenameForSign(filename string) string {
	if utf8.RuneCountInString(filename) <= maxSignedFilenameRunes {
		return filename
	}
	return string([]rune(filename)[:maxSignedFilenameRunes])
}
