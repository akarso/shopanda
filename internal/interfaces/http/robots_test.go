package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestRobotsHandler_Serve(t *testing.T) {
	h := shophttp.NewRobotsHandler("https://example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	h.Serve().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "User-agent: *") {
		t.Error("expected User-agent directive")
	}
	if !strings.Contains(body, "Allow: /") {
		t.Error("expected Allow directive")
	}
	if !strings.Contains(body, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("expected Sitemap directive, got:\n%s", body)
	}
}
