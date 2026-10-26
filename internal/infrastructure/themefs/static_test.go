package themefs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/themefs"
)

func TestStaticHandler_ChildWinsParentFallback(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(parent, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "static", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(child, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(child, "static", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, "theme.yaml"), "name: parent\nversion: 0.1.0\n")
	writeFile(t, filepath.Join(parent, "templates", "page.html"), "{{ define \"content\" }}p{{ end }}\n")
	writeFile(t, filepath.Join(parent, "static", "css", "style.css"), "parent-style")
	writeFile(t, filepath.Join(parent, "static", "css", "custom.css"), "parent-custom")
	writeFile(t, filepath.Join(child, "theme.yaml"), "name: child\nversion: 0.1.0\nparent: ../parent\n")
	writeFile(t, filepath.Join(child, "templates", "page.html"), "{{ define \"content\" }}c{{ end }}\n")
	writeFile(t, filepath.Join(child, "static", "css", "custom.css"), "child-custom")

	resolved, _, err := themefs.ResolveRootTheme(child)
	if err != nil {
		t.Fatalf("ResolveRootTheme: %v", err)
	}
	if len(resolved.StaticRoots) != 2 {
		t.Fatalf("static roots = %d, want 2", len(resolved.StaticRoots))
	}

	h := themefs.StaticHandler(func() []string { return resolved.StaticRoots }, false)
	mux := http.NewServeMux()
	mux.Handle("GET /static/{path...}", h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/custom.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("custom status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "child-custom" {
		t.Fatalf("custom body = %q, want child-custom", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("style status = %d", rec.Code)
	}
	body, _ = io.ReadAll(rec.Body)
	if string(body) != "parent-style" {
		t.Fatalf("style body = %q, want parent-style", body)
	}
}

func TestStaticURL_ChangesAfterContentEdit(t *testing.T) {
	root := t.TempDir()
	staticDir := filepath.Join(root, "static")
	if err := os.MkdirAll(filepath.Join(staticDir, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(staticDir, "css", "style.css")
	writeFile(t, css, "one")
	first := themefs.StaticURL([]string{staticDir}, "css/style.css")
	if !strings.HasPrefix(first, "/static/css/style.css?v=") {
		t.Fatalf("first = %q", first)
	}
	writeFile(t, css, "two")
	second := themefs.StaticURL([]string{staticDir}, "css/style.css")
	if first == second {
		t.Fatalf("content edit must change cache-buster: %q", first)
	}
}

func TestStaticHandler_RejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	themeDir := filepath.Join(root, "theme")
	secret := filepath.Join(root, "secret.txt")
	if err := os.MkdirAll(filepath.Join(themeDir, "static", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, secret, "top-secret")
	writeFile(t, filepath.Join(themeDir, "static", "css", "ok.css"), "ok")
	if err := os.Symlink(secret, filepath.Join(themeDir, "static", "css", "leak.css")); err != nil {
		t.Fatal(err)
	}

	roots := []string{filepath.Join(themeDir, "static")}
	h := themefs.StaticHandler(func() []string { return roots }, true)

	if _, err := themefs.ResolveStaticFile(roots, "../secret.txt"); err == nil {
		t.Fatal("ResolveStaticFile must reject parent escape")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/escaped", nil)
	req.SetPathValue("path", "../secret.txt")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dot-dot status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/static/css/leak.css", nil)
	req.SetPathValue("path", "css/leak.css")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("symlink-out status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") == "no-store" && rec.Code == http.StatusOK {
		t.Fatal("escaped file must not be served")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/static/css/ok.css", nil)
	req.SetPathValue("path", "css/ok.css")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ok status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dev Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
}

func TestStaticHandler_RejectsIntermediateDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	themeDir := filepath.Join(root, "theme")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(themeDir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "secret.txt"), "top-secret")
	if err := os.Symlink(outside, filepath.Join(themeDir, "static", "leak")); err != nil {
		t.Fatal(err)
	}
	roots := []string{filepath.Join(themeDir, "static")}
	if _, err := themefs.ResolveStaticFile(roots, "leak/secret.txt"); err == nil {
		t.Fatal("directory symlink escape must be rejected")
	}
	h := themefs.StaticHandler(func() []string { return roots }, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/leak/secret.txt", nil)
	req.SetPathValue("path", "leak/secret.txt")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}
