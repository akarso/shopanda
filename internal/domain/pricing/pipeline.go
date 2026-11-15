package pricing

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
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
// Plugin-write trust is stripped so pricing steps (including plugin-registered
// ones) cannot inherit a grant from the caller.
func (p Pipeline) Execute(ctx context.Context, pctx *PricingContext) error {
	ctx = pluginwrite.Strip(ctx)
	for _, step := range p.steps {
		if err := step.Apply(ctx, pctx); err != nil {
			return fmt.Errorf("pipeline: step %q: %w", step.Name(), err)
		}
	}
	return nil
}
