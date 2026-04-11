package rule

import "fmt"

// Executor runs rules sequentially against a context of type T.
// For each rule whose Condition evaluates to true, Apply is called.
// Execution halts on the first error.
type Executor[T any] struct {
	rules []Rule[T]
}

// NewExecutor creates an Executor from the given rules.
func NewExecutor[T any](rules ...Rule[T]) Executor[T] {
	return Executor[T]{rules: rules}
}

// Execute evaluates each rule in order. If a rule's condition matches,
// its Apply function is called with a pointer to ctx. The first error
// returned by an Apply function halts execution.
func (e Executor[T]) Execute(ctx *T) error {
	for _, r := range e.rules {
		if r.Condition.Evaluate(*ctx) {
			if err := r.Apply(ctx); err != nil {
				return fmt.Errorf("rule %q: %w", r.Name, err)
			}
		}
	}
	return nil
}
