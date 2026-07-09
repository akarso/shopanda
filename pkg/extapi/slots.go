package extapi

// Placement describes where renderer HTML is injected relative to a slot anchor.
type Placement string

const (
	PlacementBefore  Placement = "before"
	PlacementAfter   Placement = "after"
	PlacementPrepend Placement = "prepend"
	PlacementAppend  Placement = "append"
)

// SlotAnchor names a stable storefront slot anchor.
type SlotAnchor string

// SlotRenderer produces HTML for a slot placement.
type SlotRenderer func(ctx *SlotRenderContext) string

// SlotRenderContext carries page data available to slot renderers.
type SlotRenderContext struct {
	Anchor string
	Data   interface{}
}

const (
	SlotLayoutHead        SlotAnchor = "layout.head"
	SlotLayoutBodyStart   SlotAnchor = "layout.body_start"
	SlotLayoutHeader      SlotAnchor = "layout.header"
	SlotLayoutNav         SlotAnchor = "layout.nav"
	SlotLayoutCategoryNav SlotAnchor = "layout.category_nav"
	SlotLayoutMain        SlotAnchor = "layout.main"
	SlotLayoutFooter      SlotAnchor = "layout.footer"
	SlotLayoutBodyEnd     SlotAnchor = "layout.body_end"
	SlotHomeHero          SlotAnchor = "home.hero"
	SlotPDPGallery        SlotAnchor = "pdp.gallery"
	SlotPDPInfo           SlotAnchor = "pdp.info"
	SlotPDPActions        SlotAnchor = "pdp.actions"
	SlotPLPToolbar        SlotAnchor = "plp.toolbar"
	SlotCartItems         SlotAnchor = "cart.items"
	SlotCartSummary       SlotAnchor = "cart.summary"
	SlotCheckoutProgress  SlotAnchor = "checkout.progress"
	SlotCheckoutPanel     SlotAnchor = "checkout.panel"
	SlotCheckoutSummary   SlotAnchor = "checkout.summary"
	SlotAccountNav        SlotAnchor = "account.nav"
)

var slotAnchorOrder = []SlotAnchor{
	SlotLayoutHead,
	SlotLayoutBodyStart,
	SlotLayoutHeader,
	SlotLayoutNav,
	SlotLayoutCategoryNav,
	SlotLayoutMain,
	SlotLayoutFooter,
	SlotLayoutBodyEnd,
	SlotHomeHero,
	SlotPDPGallery,
	SlotPDPInfo,
	SlotPDPActions,
	SlotPLPToolbar,
	SlotCartItems,
	SlotCartSummary,
	SlotCheckoutProgress,
	SlotCheckoutPanel,
	SlotCheckoutSummary,
	SlotAccountNav,
}

// SlotAnchors returns documented stable slot anchors.
func SlotAnchors() []SlotAnchor {
	out := make([]SlotAnchor, len(slotAnchorOrder))
	copy(out, slotAnchorOrder)
	return out
}

// SlotAnchorNames returns slot anchor wire names in catalog order.
func SlotAnchorNames() []string {
	anchors := SlotAnchors()
	out := make([]string, len(anchors))
	for i, anchor := range anchors {
		out[i] = string(anchor)
	}
	return out
}
