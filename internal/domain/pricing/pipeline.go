package pricing

import (
	"context"
	"fmt"
)

// Pipeline runs pricing steps sequentially against a PricingContext.
type Pipeline struct {
	steps []PricingStep
}

// NewPipeline creates a Pipeline from the given steps.
func NewPipeline(steps ...PricingStep) Pipeline {
	return Pipeline{steps: steps}
}

// Execute runs each step in order. An error from any step halts the pipeline.
func (p Pipeline) Execute(ctx context.Context, pctx *PricingContext) error {
	for _, step := range p.steps {
		if err := step.Apply(ctx, pctx); err != nil {
			return fmt.Errorf("pipeline: step %q: %w", step.Name(), err)
		}
	}
	return nil
}
