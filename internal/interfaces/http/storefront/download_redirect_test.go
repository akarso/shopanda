package storefront

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
)

type redirectStubStorage struct{ name string }

func (s redirectStubStorage) Name() string                 { return s.name }
func (s redirectStubStorage) Save(string, io.Reader) error { return nil }
func (s redirectStubStorage) Delete(string) error          { return nil }
func (s redirectStubStorage) URL(string) string            { return "" }

var _ media.Storage = redirectStubStorage{}

func TestIsSafeLocalSignedPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		ok   bool
	}{
		{"/api/v1/media/local-signed/abcTOKEN", true},
		{"/api/v1/media/local-signed/", false},
		{"/api/v1/media/local-signed", false},
		{"/api/v1/media/local-signed/../admin", false},
		{"/api/v1/media/local-signed/%2e%2e/admin", false},
		{"/api/v1/media/local-signed/%2E%2E", false},
		{"/api/v1/media/local-signed/tok/extra", false},
		{"/api/v1/media/local-signed/tok\\x", false},
		{"/api/v1/admin", false},
		{"https://evil.example/api/v1/media/local-signed/x", false},
	}
	for _, tc := range cases {
		if got := isSafeLocalSignedPath(tc.path); got != tc.ok {
			t.Errorf("isSafeLocalSignedPath(%q) = %v, want %v", tc.path, got, tc.ok)
		}
	}
}

func TestValidateSignedRedirectURL_LocalRelative(t *testing.T) {
	t.Parallel()
	local := redirectStubStorage{name: "local"}
	if err := validateSignedRedirectURL("/api/v1/media/local-signed/goodToken", local); err != nil {
		t.Fatalf("happy: %v", err)
	}
	rejects := []string{
		"/api/v1/media/local-signed/../admin",
		"/api/v1/media/local-signed/%2e%2e/admin",
		"/api/v1/media/local-signed/a/b",
		"//evil.example/api/v1/media/local-signed/x",
		"http://evil.example/api/v1/media/local-signed/x",
		"/api/v1/media/local-signed/x?q=1",
		"/api/v1/media/local-signed/x#frag",
	}
	for _, raw := range rejects {
		if err := validateSignedRedirectURL(raw, local); err == nil {
			t.Errorf("expected reject for %q", raw)
		}
	}
}
