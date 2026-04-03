package checkout

// Step is a unit of work in the checkout workflow.
type Step interface {
	// Name returns a unique identifier for the step (e.g. "validate_cart").
	Name() string

	// Execute performs the step's work, reading and mutating the Context.
	// Returning an error stops the workflow.
	Execute(ctx *Context) error
}
