package storefront

import (
	"strings"
	"testing"
)

func TestFragmentHelper_RendersSkeletonAndEndpoint(t *testing.T) {
	h := &fragmentHelper{}
	html := string(h.render("csrf"))
	if !strings.Contains(html, `hx-get="/fragments/csrf"`) {
		t.Fatalf("csrf fragment = %q", html)
	}
	if !strings.Contains(html, `name="csrf_token"`) || !strings.Contains(html, `value=""`) {
		t.Fatalf("csrf skeleton must be empty token field: %q", html)
	}

	mini := string(h.render("mini-cart"))
	if !strings.Contains(mini, `hx-get="/fragments/mini-cart"`) {
		t.Fatalf("mini-cart = %q", mini)
	}
	if !strings.Contains(mini, "cart-updated from:body") || !strings.Contains(mini, "mini-cart-shell") {
		t.Fatalf("mini-cart must keep cart-updated trigger and shell class: %q", mini)
	}

	unknown := string(h.render("nope"))
	if !strings.Contains(unknown, "unknown") {
		t.Fatalf("unknown fragment = %q", unknown)
	}
}
