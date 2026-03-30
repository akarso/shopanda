package composition

// Block represents a UI-agnostic component attached to a composition context.
type Block struct {
	Type string
	Data map[string]interface{}
}
