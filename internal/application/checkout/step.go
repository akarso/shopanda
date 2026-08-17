package checkout

import "context"

// Step is a unit of work in the checkout workflow.
type Step interface {
	// Name returns a unique identifier for the step (e.g. "validate_cart").
	Name() string

	// Execute performs the step's work, reading and mutating the checkout Context.
	// ctx is the request/workflow context and must be passed to forward-progress
	// blocking calls. Compensating/rollback writes and persistence of already-committed
	// external side effects must not reuse a canceled parent (see detachedTimeout).
	// Prefer returning cancel/deadline errors with %w so callers can use errors.Is
	// (convention; not centrally enforced).
	Execute(ctx context.Context, cctx *Context) error
}
