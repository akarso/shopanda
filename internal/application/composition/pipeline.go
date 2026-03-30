package composition

import "fmt"

// Pipeline executes a sequence of steps against a typed context.
type Pipeline[T any] struct {
	steps []Step[T]
}

// NewPipeline creates an empty pipeline.
func NewPipeline[T any]() *Pipeline[T] {
	return &Pipeline[T]{}
}

// AddStep appends a step to the end of the pipeline.
func (p *Pipeline[T]) AddStep(s Step[T]) {
	p.steps = append(p.steps, s)
}

// Execute runs all steps in order against ctx.
// If any step returns an error the pipeline stops and returns it.
func (p *Pipeline[T]) Execute(ctx *T) error {
	for _, s := range p.steps {
		if err := s.Apply(ctx); err != nil {
			return fmt.Errorf("pipeline step %q: %w", s.Name(), err)
		}
	}
	return nil
}

// Steps returns the names of all registered steps in order.
func (p *Pipeline[T]) Steps() []string {
	names := make([]string, len(p.steps))
	for i, s := range p.steps {
		names[i] = s.Name()
	}
	return names
}
