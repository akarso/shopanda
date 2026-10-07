package storefront

import (
	"strings"
	"testing"

	cacheapp "github.com/akarso/shopanda/internal/application/cache"
)

func TestApplyCSPNonceRotation_HTMLEscapedPlus(t *testing.T) {
	old := "abc+def/ghi+jkl"
	next := "mno+pqr/stu"
	html := `<meta name="csp-nonce" content="` + cspNonceHTMLForm(old) + `">` +
		`<script nonce="` + cspNonceHTMLForm(old) + `" src="/x.js"></script>`
	csp := storefrontCSPHeader(old)

	gotHTML, gotCSP := applyCSPNonceRotation(html, csp, old, next)

	if strings.Contains(gotHTML, cspNonceHTMLForm(old)) || strings.Contains(gotHTML, old) {
		t.Fatalf("HTML still contains old nonce: %s", gotHTML)
	}
	if !strings.Contains(gotHTML, cspNonceHTMLForm(next)) {
		t.Fatalf("HTML missing escaped new nonce: %s", gotHTML)
	}
	if strings.Contains(gotHTML, next) {
		t.Fatalf("HTML must not embed raw new nonce with +: %s", gotHTML)
	}
	if gotCSP != storefrontCSPHeader(next) {
		t.Fatalf("CSP = %q, want %q", gotCSP, storefrontCSPHeader(next))
	}
}

func TestRotateStoredCSPNonce_FixedPlusNonce(t *testing.T) {
	old := "fixed+nonce/value"
	next := "rotated+nonce/value"
	prev := cspNonceTestHook
	cspNonceTestHook = func() string { return next }
	t.Cleanup(func() { cspNonceTestHook = prev })

	entry := cacheapp.PageEntry{
		HTML:  `<meta content="` + cspNonceHTMLForm(old) + `"><script nonce="` + cspNonceHTMLForm(old) + `"></script>`,
		CSP:   storefrontCSPHeader(old),
		Nonce: old,
	}
	html, csp, ok := rotateStoredCSPNonce(entry)
	if !ok {
		t.Fatal("rotate should succeed")
	}
	if strings.Contains(html, "fixed") || strings.Contains(csp, "fixed") {
		t.Fatalf("old nonce leaked: html=%q csp=%q", html, csp)
	}
	if !strings.Contains(html, cspNonceHTMLForm(next)) {
		t.Fatalf("HTML missing escaped rotated nonce: %q", html)
	}
	if csp != storefrontCSPHeader(next) {
		t.Fatalf("CSP = %q, want header with raw rotated nonce", csp)
	}
}
