package pimdemo

import (
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// EnrichmentStep enriches PDP responses from an external PIM GraphQL API.
type EnrichmentStep struct {
	fetcher *enrichmentFetcher
	cache   *ttlCache
	log     logger.Logger
}

// NewEnrichmentStep returns a PDP composition step with optional in-memory caching.
func NewEnrichmentStep(fetcher *enrichmentFetcher, cache *ttlCache, log logger.Logger) *EnrichmentStep {
	if fetcher == nil {
		panic("pimdemo: enrichment fetcher must not be nil")
	}
	if cache == nil {
		panic("pimdemo: enrichment cache must not be nil")
	}
	return &EnrichmentStep{fetcher: fetcher, cache: cache, log: log}
}

func (s *EnrichmentStep) Name() string { return StepName }

func (s *EnrichmentStep) Apply(ctx *composition.ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}
	slug := ctx.Product.Slug
	if slug == "" {
		return nil
	}

	if data, ok := s.cache.Get(slug); ok {
		s.attachBlock(ctx, data, true)
		return nil
	}

	data, err := s.fetcher.Fetch(ctx.Ctx, slug)
	if err != nil {
		if s.log != nil {
			s.log.Warn("pimdemo enrichment fetch failed", map[string]interface{}{
				"slug":  slug,
				"error": err.Error(),
			})
		}
		return nil
	}
	s.cache.Set(slug, data)
	s.attachBlock(ctx, data, false)
	return nil
}

func (s *EnrichmentStep) attachBlock(ctx *composition.ProductContext, data EnrichmentData, cached bool) {
	if !data.HasContent() {
		return
	}
	specs := make([]map[string]interface{}, 0, len(data.Specs))
	for _, spec := range data.Specs {
		specs = append(specs, map[string]interface{}{
			"key":   spec.Key,
			"value": spec.Value,
		})
	}
	ctx.Blocks = append(ctx.Blocks, composition.Block{
		Type: BlockType,
		Data: map[string]interface{}{
			"marketing_title":       data.MarketingTitle,
			"marketing_description": data.MarketingDescription,
			"specs":                 specs,
			"cached":                cached,
		},
	})
}
