package http_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assetsApp "github.com/akarso/shopanda/internal/application/assets"
	"github.com/akarso/shopanda/internal/domain/theme"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestStorefront_PluginAssetGatedByRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: asset-test\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	layout := `<!DOCTYPE html><html><head>{{ range .Layout.Assets.HeadCSS }}<link rel="stylesheet" href="{{ .URL }}">{{ end }}{{ range .Layout.Assets.HeadJS }}<script src="{{ .URL }}"></script>{{ end }}</head><body>{{ template "content" . }}{{ range .Layout.Assets.FooterJS }}<script src="{{ .URL }}"></script>{{ end }}</body></html>`
	home := `{{ define "content" }}<h1>Home</h1>{{ end }}{{ template "layout.html" . }}`
	cart := `{{ define "content" }}<h1>Cart</h1>{{ end }}{{ template "layout.html" . }}`
	for name, body := range map[string]string{"layout.html": layout, "home.html": home, "cart.html": cart} {
		if err := os.WriteFile(filepath.Join(tplDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	reg := assetsApp.NewRegistry()
	if err := reg.Register("plugin.cart", assetsApp.Manifest{
		Path: "/plugins/cart.js", Kind: assetsApp.KindJS, Placement: assetsApp.PlacementFooter,
		Routes: []string{"/cart"}, Priority: 100,
	}); err != nil {
		t.Fatal(err)
	}

	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, &mockStorefrontCategoryRepo{}, nil, nil, nil).
		WithAssets(reg)

	homeRec := httptest.NewRecorder()
	h.Home()(homeRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(homeRec.Body.String(), "/plugins/cart.js") {
		t.Fatalf("home page should not include cart asset:\n%s", homeRec.Body.String())
	}

	cartRec := httptest.NewRecorder()
	h.Cart()(cartRec, httptest.NewRequest(http.MethodGet, "/cart", nil))
	if !strings.Contains(cartRec.Body.String(), `<script src="/plugins/cart.js"></script>`) {
		t.Fatalf("cart page missing plugin asset:\n%s", cartRec.Body.String())
	}
}

func TestStorefront_PluginAssetCSPNonceWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: asset-test\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	layout := `<!DOCTYPE html><html><head></head><body>{{ template "content" . }}{{ range .Layout.Assets.FooterJS }}<script src="{{ .URL }}"{{ if $.Layout.CSPEnabled }} nonce="{{ $.Layout.CSPNonce }}"{{ end }}></script>{{ end }}</body></html>`
	home := `{{ define "content" }}<h1>Home</h1>{{ end }}{{ template "layout.html" . }}`
	for name, body := range map[string]string{"layout.html": layout, "home.html": home} {
		if err := os.WriteFile(filepath.Join(tplDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	reg := assetsApp.NewRegistry()
	_ = reg.Register("plugin.demo", assetsApp.Manifest{
		Path: "/plugins/demo.js", Kind: assetsApp.KindJS, Placement: assetsApp.PlacementFooter,
	})

	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, &mockStorefrontCategoryRepo{}, nil, nil, nil).
		WithAssets(reg).
		WithCSPEnabled(true)

	rec := httptest.NewRecorder()
	h.Home()(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, `nonce="`) {
		t.Fatalf("expected nonce attribute on injected script:\n%s", body)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" || !strings.Contains(csp, "nonce-") {
		t.Fatalf("CSP header = %q, want script-src with nonce", csp)
	}
	if !strings.Contains(csp, "object-src 'none'") || !strings.Contains(csp, "base-uri 'self'") {
		t.Fatalf("CSP header = %q, want hardened directives", csp)
	}
}

func TestStorefront_PluginAssetsStableOrderAcrossPlugins(t *testing.T) {
	reg := assetsApp.NewRegistry()
	_ = reg.Register("plugin.z", assetsApp.Manifest{
		Path: "/z.js", Kind: assetsApp.KindJS, Placement: assetsApp.PlacementHead, Priority: 200,
	})
	_ = reg.Register("plugin.a", assetsApp.Manifest{
		Path: "/a.js", Kind: assetsApp.KindJS, Placement: assetsApp.PlacementHead, Priority: 100,
	})

	got := reg.ForRoute("/")
	if len(got.HeadJS) != 2 || got.HeadJS[0].URL != "/a.js" || got.HeadJS[1].URL != "/z.js" {
		t.Fatalf("HeadJS order = %+v, want a then z", got.HeadJS)
	}
}
