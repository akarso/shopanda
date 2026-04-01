package pricing

// PricingStep represents a single step in the pricing pipeline.
// Steps are executed sequentially; each step mutates the PricingContext.
// A step that returns an error halts the pipeline.
type PricingStep interface {
	Name() string
	Apply(ctx *PricingContext) error
}
