package extapi

import "context"

// CompositionBlock is a named payload attached to a composed page response.
type CompositionBlock struct {
	Type string
	Data map[string]interface{}
}

// ProductCompositionContext is the public PDP composition view.
type ProductCompositionContext interface {
	Context() context.Context
	ProductID() string
	ProductSlug() string
	Currency() string
	Country() string
	StoreID() string
	AddBlock(block CompositionBlock)
	MetaGet(key string) (interface{}, bool)
	MetaSet(key string, value interface{})
}

// ProductCompositionStep is a public PDP composition pipeline step.
type ProductCompositionStep interface {
	Name() string
	Apply(ctx ProductCompositionContext) error
}
