package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestDocsHandler_UI(t *testing.T) {
	handler := shophttp.NewDocsHandler([]byte("openapi: '3.0.3'"))

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	handler.UI().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html; charset=utf-8, got %s", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Fatal("expected HTML to contain swagger-ui reference")
	}
	if !strings.Contains(body, "/docs/openapi.yaml") {
		t.Fatal("expected HTML to reference /docs/openapi.yaml")
	}
}

func TestDocsHandler_Spec(t *testing.T) {
	spec := []byte("openapi: '3.0.3'\ninfo:\n  title: Test\n  version: '1.0.0'\n")
	handler := shophttp.NewDocsHandler(spec)

	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	handler.Spec().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/yaml" {
		t.Fatalf("expected application/yaml, got %s", ct)
	}

	body := rec.Body.String()
	if body != string(spec) {
		t.Fatalf("expected spec bytes to match, got %q", body)
	}
}

func TestDocsHandler_EmptySpec(t *testing.T) {
	handler := shophttp.NewDocsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	handler.Spec().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body for nil spec, got %d bytes", rec.Body.Len())
	}
}
