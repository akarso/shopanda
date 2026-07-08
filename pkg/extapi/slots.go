package extapi

// Slot placement values (Stable v0).
type Placement string

const (
	PlacementBefore  Placement = "before"
	PlacementAfter   Placement = "after"
	PlacementPrepend Placement = "prepend"
	PlacementAppend  Placement = "append"
)

// Stable v0 slot anchors for the default theme catalog.
const (
	SlotLayoutHead        = "layout.head"
	SlotLayoutBodyStart   = "layout.body_start"
	SlotLayoutHeader      = "layout.header"
	SlotLayoutNav         = "layout.nav"
	SlotLayoutCategoryNav = "layout.category_nav"
	SlotLayoutMain        = "layout.main"
	SlotLayoutFooter      = "layout.footer"
	SlotLayoutBodyEnd     = "layout.body_end"
	SlotPDPGallery        = "pdp.gallery"
	SlotPDPInfo           = "pdp.info"
	SlotPDPActions        = "pdp.actions"
	SlotPLPToolbar        = "plp.toolbar"
	SlotCartItems         = "cart.items"
	SlotCartSummary       = "cart.summary"
	SlotCheckoutProgress  = "checkout.progress"
	SlotCheckoutSummary   = "checkout.summary"
)

// SlotAnchors returns documented stable slot anchor names.
func SlotAnchors() []string {
	return []string{
		SlotLayoutHead,
		SlotLayoutBodyStart,
		SlotLayoutHeader,
		SlotLayoutNav,
		SlotLayoutCategoryNav,
		SlotLayoutMain,
		SlotLayoutFooter,
		SlotLayoutBodyEnd,
		SlotPDPGallery,
		SlotPDPInfo,
		SlotPDPActions,
		SlotPLPToolbar,
		SlotCartItems,
		SlotCartSummary,
		SlotCheckoutProgress,
		SlotCheckoutSummary,
	}
}
