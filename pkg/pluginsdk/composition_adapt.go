package pluginsdk

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AdaptProductCompositionStep wraps a public PDP step as composition.Step[ProductContext].
func AdaptProductCompositionStep(step extapi.ProductCompositionStep) composition.Step[composition.ProductContext] {
	if step == nil {
		panic("pluginsdk: AdaptProductCompositionStep: step must not be nil")
	}
	return &productCompositionBridge{step: step}
}

type productCompositionBridge struct {
	step extapi.ProductCompositionStep
}

func (b *productCompositionBridge) Name() string { return b.step.Name() }

func (b *productCompositionBridge) Apply(ctx *composition.ProductContext) error {
	return b.step.Apply(&productCompositionView{ctx: ctx})
}

type productCompositionView struct {
	ctx *composition.ProductContext
}

func (v *productCompositionView) Context() context.Context {
	if v.ctx == nil || v.ctx.Ctx == nil {
		return context.Background()
	}
	return v.ctx.Ctx
}

func (v *productCompositionView) ProductID() string {
	if v.ctx == nil || v.ctx.Product == nil {
		return ""
	}
	return v.ctx.Product.ID
}

func (v *productCompositionView) ProductSlug() string {
	if v.ctx == nil || v.ctx.Product == nil {
		return ""
	}
	return v.ctx.Product.Slug
}

func (v *productCompositionView) Currency() string { return v.ctx.Currency }
func (v *productCompositionView) Country() string  { return v.ctx.Country }
func (v *productCompositionView) StoreID() string  { return v.ctx.StoreID }

func (v *productCompositionView) AddBlock(block extapi.CompositionBlock) {
	v.ctx.Blocks = append(v.ctx.Blocks, composition.Block{
		Type: block.Type,
		Data: block.Data,
	})
}

func (v *productCompositionView) MetaGet(key string) (interface{}, bool) {
	if v.ctx.Meta == nil {
		return nil, false
	}
	val, ok := v.ctx.Meta[key]
	return val, ok
}

func (v *productCompositionView) MetaSet(key string, value interface{}) {
	if v.ctx.Meta == nil {
		v.ctx.Meta = make(map[string]interface{})
	}
	v.ctx.Meta[key] = value
}

// coerceCompositionStep validates/adapts step for pipeline. For "pdp" it
// accepts the public extapi.ProductCompositionStep (adapted) or an
// already-domain composition.Step[ProductContext] (dual-mode internal
// plugins). "plp" has no public contract yet, so it only accepts the domain
// composition.Step[ListingContext]. Any other pipeline name is unknown to the
// SDK and errors fail-closed (same as a bad pdp/plp step type) rather than
// passing through unchanged: a typo'd pipeline name would otherwise only
// surface much later as a dropped step at wiring time, never during the
// plugin's own Init.
func coerceCompositionStep(pipeline string, step any) (any, error) {
	if step == nil {
		return nil, fmt.Errorf("pluginsdk: composition step must not be nil")
	}
	switch pipeline {
	case "pdp":
		if s, ok := step.(extapi.ProductCompositionStep); ok {
			return AdaptProductCompositionStep(s), nil
		}
		if _, ok := step.(composition.Step[composition.ProductContext]); ok {
			return step, nil
		}
		return nil, fmt.Errorf("pluginsdk: expected extapi.ProductCompositionStep for pipeline %q, got %T", pipeline, step)
	case "plp":
		if _, ok := step.(composition.Step[composition.ListingContext]); ok {
			return step, nil
		}
		return nil, fmt.Errorf("pluginsdk: expected composition.Step[ListingContext] for pipeline %q, got %T", pipeline, step)
	default:
		return nil, fmt.Errorf("pluginsdk: unknown composition pipeline %q", pipeline)
	}
}
