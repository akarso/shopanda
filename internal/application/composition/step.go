package composition

// Step defines a single transformation in a composition pipeline.
// Each step operates on a typed context pointer.
type Step[T any] interface {
	Name() string
	Apply(ctx *T) error
}
