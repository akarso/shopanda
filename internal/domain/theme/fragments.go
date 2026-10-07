package theme

import (
	"fmt"
	"html"
	"html/template"
	"log"
	"strings"
	"sync"
)

// Known fragment names for the ESI-equivalent helper (PR-1045).
// Themes call {{fragment "csrf"}} etc.; unknown names render an empty comment.
const (
	FragmentCSRF           = "csrf"
	FragmentGreeting       = "greeting"
	FragmentCartCount      = "cart-count"
	FragmentMiniCart       = "mini-cart"
	FragmentRecentlyViewed = "recently-viewed"
)

// fragmentPaths maps helper names to storefront fragment URLs.
var fragmentPaths = map[string]string{
	FragmentCSRF:           "/fragments/csrf",
	FragmentGreeting:       "/fragments/greeting",
	FragmentCartCount:      "/fragments/cart-count",
	FragmentMiniCart:       "/fragments/mini-cart",
	FragmentRecentlyViewed: "/fragments/recently-viewed",
	// Spec-style aliases.
	"minicart":         "/fragments/mini-cart",
	"recently_viewed":  "/fragments/recently-viewed",
	"account-greeting": "/fragments/greeting",
}

// fragmentSkeletons are the required loading-state markup for each fragment.
// CSRF skeleton uses an empty value so full-page cache can store the shell.
var fragmentSkeletons = map[string]string{
	FragmentCSRF:           `<input type="hidden" name="csrf_token" value="" aria-hidden="true">`,
	FragmentGreeting:       `<strong class="account-greeting">Account</strong>`,
	FragmentCartCount:      `Cart (0)`,
	FragmentMiniCart:       `<p class="muted-note">Loading cart…</p>`,
	FragmentRecentlyViewed: `<section class="recently-viewed" aria-busy="true"><p class="muted-note">Loading recently viewed…</p></section>`,
	"minicart":             `<p class="muted-note">Loading cart…</p>`,
	"recently_viewed":      `<section class="recently-viewed" aria-busy="true"><p class="muted-note">Loading recently viewed…</p></section>`,
	"account-greeting":     `<strong class="account-greeting">Account</strong>`,
}

// fragmentHelper is per-engine so unknown-name warning state is not process-global.
type fragmentHelper struct {
	devWarn bool
	unknown sync.Map // name → struct{}
}

func fragmentFuncMap(devWarn bool) template.FuncMap {
	h := &fragmentHelper{devWarn: devWarn}
	return template.FuncMap{
		"fragment": h.render,
	}
}

// render emits an htmx hole for a named fragment.
// Optional second arg is a CSS id for the wrapper (defaults to fragment-{name}).
func (h *fragmentHelper) render(args ...interface{}) template.HTML {
	if len(args) == 0 {
		return template.HTML("<!-- fragment: missing name -->")
	}
	name := strings.TrimSpace(fmt.Sprint(args[0]))
	if name == "" {
		return template.HTML("<!-- fragment: empty name -->")
	}
	path, ok := fragmentPaths[name]
	if !ok {
		if h != nil && h.devWarn {
			if _, seen := h.unknown.LoadOrStore(name, struct{}{}); !seen {
				log.Printf("theme.fragment.unknown name=%q", name)
			}
		}
		return template.HTML(fmt.Sprintf("<!-- fragment: unknown %s -->", html.EscapeString(name)))
	}
	id := "fragment-" + name
	if len(args) > 1 {
		if raw := strings.TrimSpace(fmt.Sprint(args[1])); raw != "" {
			id = raw
		}
	}
	skeleton := fragmentSkeletons[name]
	trigger := "load"
	swap := "outerHTML"
	tag := "div"
	class := "storefront-fragment"
	switch name {
	case FragmentCSRF:
		tag = "span"
		class = ""
	case FragmentCartCount:
		tag = "span"
		class = ""
		swap = "innerHTML"
		trigger = "load, cart-updated from:body"
	case FragmentMiniCart, "minicart":
		class = "mini-cart-shell"
		swap = "innerHTML"
		trigger = "load, cart-updated from:body"
	}
	classAttr := ""
	if class != "" {
		classAttr = ` class="` + html.EscapeString(class) + `"`
	}
	return template.HTML(fmt.Sprintf(
		`<%s id="%s"%s hx-get="%s" hx-trigger="%s" hx-swap="%s">%s</%s>`,
		tag, html.EscapeString(id), classAttr, html.EscapeString(path), trigger, swap, skeleton, tag,
	))
}
