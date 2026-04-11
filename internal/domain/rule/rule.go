package rule

// Rule pairs a Condition with an action that mutates a context of type T.
// Name identifies the rule in error messages and logging.
type Rule[T any] struct {
	Name      string
	Condition Condition[T]
	Apply     func(ctx *T) error
}
