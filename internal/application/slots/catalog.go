package slots

// Group names for standard storefront slot anchors.
const (
	GroupLayout   = "layout"
	GroupPDP      = "pdp"
	GroupPLP      = "plp"
	GroupCart     = "cart"
	GroupCheckout = "checkout"
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
		{Name: "pdp.gallery", Group: GroupPDP, Description: "PDP media area"},
		{Name: "pdp.info", Group: GroupPDP, Description: "PDP product info column"},
		{Name: "pdp.actions", Group: GroupPDP, Description: "PDP add-to-cart actions"},
		{Name: "plp.toolbar", Group: GroupPLP, Description: "Category / product list toolbar"},
		{Name: "cart.items", Group: GroupCart, Description: "Cart line items table"},
		{Name: "cart.summary", Group: GroupCart, Description: "Cart summary aside"},
		{Name: "checkout.progress", Group: GroupCheckout, Description: "Checkout step indicator"},
		{Name: "checkout.summary", Group: GroupCheckout, Description: "Checkout order summary aside"},
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
