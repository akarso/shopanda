package rule

// Condition evaluates whether a rule should fire against the given context.
// Implementations must be side-effect-free.
type Condition[T any] interface {
	Evaluate(ctx T) bool
}

// ConditionFunc adapts a plain function to the Condition interface.
type ConditionFunc[T any] func(ctx T) bool

// Evaluate calls the underlying function.
func (f ConditionFunc[T]) Evaluate(ctx T) bool { return f(ctx) }
