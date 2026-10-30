package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// PrivateDownloadsPrefix is the storage key prefix for downloadable grant
// targets (PR-1077). Objects under this prefix must not get public
// Cache-Control or public ACL on upload. Delivery uses signed URLs or
// ReadableStorage — never permanent Storage.URL().
const PrivateDownloadsPrefix = "private/downloads/"

// IsPrivateDownloadsPath reports whether path is under PrivateDownloadsPrefix.
func IsPrivateDownloadsPath(path string) bool {
	p := strings.TrimLeft(strings.TrimSpace(path), "/")
	return strings.HasPrefix(p, PrivateDownloadsPrefix)
}

// SanitizeContentDispositionFilename returns an ASCII-safe filename for the
// Content-Disposition filename= parameter. Non-ASCII and header-unsafe runes
// are stripped; empty results become "download".
func SanitizeContentDispositionFilename(filename string) string {
	ascii := asciiContentDispositionFilename(filename)
	if ascii == "" {
		return "download"
	}
	return ascii
}

// ContentDispositionAttachment builds a Content-Disposition attachment value
// with an ASCII filename= fallback and, when the name contains non-ASCII,
// an RFC 5987 filename* parameter (UTF-8 percent-encoded).
func ContentDispositionAttachment(filename string) string {
	trimmed := strings.TrimSpace(filename)
	encoded := stripContentDispositionUnsafe(trimmed)
	ascii := asciiContentDispositionFilename(trimmed)
	if ascii == "" {
		ascii = "download"
	}
	disp := fmt.Sprintf(`attachment; filename="%s"`, ascii)
	if encoded == "" || !containsNonASCII(encoded) {
		return disp
	}
	return disp + "; filename*=UTF-8''" + percentEncodeRFC5987(encoded)
}

func stripContentDispositionUnsafe(filename string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\r', '\n', '\\':
			return -1
		default:
			return r
		}
	}, filename)
}

func asciiContentDispositionFilename(filename string) string {
	return strings.Map(func(r rune) rune {
		if r > 0x7f {
			return -1
		}
		switch r {
		case '"', '\r', '\n', '\\':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(filename))
}

func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return true
		}
	}
	return false
}

// percentEncodeRFC5987 percent-encodes a UTF-8 string for RFC 5987 filename*
// (attr-char left unescaped; all other octets as %XX).
func percentEncodeRFC5987(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isRFC5987AttrChar(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isRFC5987AttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

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
	// Must not be used for downloadable grant delivery (PR-1077) — that URL
	// may be permanent and would bypass grant expiry/limit enforcement.
	URL(path string) string
}

// ReadableStorage extends Storage with byte retrieval for admin-proxied
// downloads and storefront grant delivery when signed URLs are unavailable
// (local filesystem).
// Implementations should return an error wrapping os.ErrNotExist when the object is absent.
type ReadableStorage interface {
	Storage

	// Read returns the stored object bytes at path.
	Read(path string) ([]byte, error)
}

// SignURLOptions configures a short-lived download URL (PR-1077).
type SignURLOptions struct {
	// TTL is the signed URL lifetime. Callers typically use ~60s and may
	// cap it to remaining grant expiry.
	TTL time.Duration
	// Filename is used for Content-Disposition: attachment on the signed GET.
	Filename string
}

// SignedURLStorage optionally provides object existence checks and short-lived
// URLs for private delivery (PR-1077). Prefer this over ReadableStorage when
// the backend supports it (e.g. S3). TTL must stay short so expiry/limit remain
// enforceable. Implementations must set response Cache-Control to
// private, no-store and Content-Disposition attachment on the signed GET —
// never rely on permanent Storage.URL().
type SignedURLStorage interface {
	Storage

	// Head verifies the object exists at path. Missing objects must return an
	// error for which IsNotFound is true so delivery can 404 without claiming
	// a download slot.
	Head(ctx context.Context, path string) error

	// SignedURL returns a time-limited URL for path. Presign failures must be
	// returned to the caller (do not fall back to permanent URL()).
	SignedURL(ctx context.Context, path string, opts SignURLOptions) (string, error)
}

// IsNotFound reports whether err indicates a missing storage object.
// Uses errors.Is so wrapped os.ErrNotExist from storage adapters matches
// (os.IsNotExist does not unwrap in current Go).
func IsNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
