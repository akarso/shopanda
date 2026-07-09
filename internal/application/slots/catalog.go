package slots

import (
	"sort"
	"strings"
)

// Group names for standard storefront slot anchors.
const (
	GroupLayout   = "layout"
	GroupHome     = "home"
	GroupPDP      = "pdp"
	GroupPLP      = "plp"
	GroupCart     = "cart"
	GroupCheckout = "checkout"
	GroupAccount  = "account"
)

// StandardAnchor documents a canonical default-theme slot anchor.
type StandardAnchor struct {
	Name        string
	Group       string
	Description string
}

// StandardAnchors returns the canonical slot catalog for the default theme.
// Custom themes should preserve these names or document replacements.
func StandardAnchors() []StandardAnchor {
	return []StandardAnchor{
		{Name: "layout.head", Group: GroupLayout, Description: "End of <head> (meta tags, inline snippets)"},
		{Name: "layout.body_start", Group: GroupLayout, Description: "Start of <body>"},
		{Name: "layout.header", Group: GroupLayout, Description: "Site header shell"},
		{Name: "layout.nav", Group: GroupLayout, Description: "Primary navigation"},
		{Name: "layout.category_nav", Group: GroupLayout, Description: "Category navigation (when categories exist)"},
		{Name: "layout.main", Group: GroupLayout, Description: "Main content wrapper"},
		{Name: "layout.footer", Group: GroupLayout, Description: "Site footer"},
		{Name: "layout.body_end", Group: GroupLayout, Description: "End of <body> (after footer scripts)"},
		{Name: "home.hero", Group: GroupHome, Description: "Home page hero area"},
		{Name: "pdp.gallery", Group: GroupPDP, Description: "PDP media area"},
		{Name: "pdp.info", Group: GroupPDP, Description: "PDP product info column"},
		{Name: "pdp.actions", Group: GroupPDP, Description: "PDP add-to-cart actions"},
		{Name: "plp.toolbar", Group: GroupPLP, Description: "Category / product list toolbar"},
		{Name: "cart.items", Group: GroupCart, Description: "Cart line items table"},
		{Name: "cart.summary", Group: GroupCart, Description: "Cart summary aside"},
		{Name: "checkout.progress", Group: GroupCheckout, Description: "Checkout step indicator"},
		{Name: "checkout.panel", Group: GroupCheckout, Description: "Checkout main form panel"},
		{Name: "checkout.summary", Group: GroupCheckout, Description: "Checkout order summary aside"},
		{Name: "account.nav", Group: GroupAccount, Description: "Signed-in account section navigation"},
	}
}

// StandardAnchorNames returns canonical anchor names in catalog order.
func StandardAnchorNames() []string {
	items := StandardAnchors()
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}

// StandardAnchorNameSet returns canonical anchor names as a lookup set.
func StandardAnchorNameSet() map[string]struct{} {
	names := StandardAnchorNames()
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

// UnknownDeclaredAnchors returns declared anchors not present in StandardAnchors(), sorted.
func UnknownDeclaredAnchors(declared []string) []string {
	standard := StandardAnchorNameSet()
	seen := make(map[string]struct{})
	var unknown []string
	for _, name := range declared {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := standard[name]; ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	return unknown
}
