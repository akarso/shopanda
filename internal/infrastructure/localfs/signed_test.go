package localfs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
)

func TestStorage_SignedURLStorage(t *testing.T) {
	t.Parallel()
	s := localfs.NewWithSecret(t.TempDir(), "/media", []byte("test-secret-32-bytes-long!!!!"))
	var _ media.SignedURLStorage = s

	ctx := context.Background()
	path := "private/downloads/file.bin"
	if err := s.Save(path, strings.NewReader("secret-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := s.Head(ctx, path); err != nil {
		t.Fatalf("Head: %v", err)
	}
	url, err := s.SignedURL(ctx, path, media.SignURLOptions{TTL: time.Minute, Filename: "file.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, localfs.SignedPathPrefix) {
		t.Fatalf("url = %q", url)
	}

	token := strings.TrimPrefix(url, localfs.SignedPathPrefix)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.SignedPathPrefix+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "secret-bytes" {
		t.Fatalf("body = %q", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.SignedPathPrefix+"bogus", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad token status = %d", rec.Code)
	}

	expired, err := s.ExpiredTokenForTest(path, "file.bin")
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, localfs.SignedPathPrefix+expired, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expired status = %d", rec.Code)
	}
}
