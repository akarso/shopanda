package extapi

// Stable v0 hook points.
const (
	HookCartAddItemAfter = "cart.add_item.after"
)

// HookPoints returns all documented stable hook point names.
func HookPoints() []string {
	return []string{HookCartAddItemAfter}
}
