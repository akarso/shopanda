package pimdemo

import (
	"context"
	"fmt"
	"strings"

	sdkgraphql "github.com/akarso/shopanda/pkg/integrationsdk/graphql"
)

// EnrichmentData is normalized PIM marketing content for one product slug.
type EnrichmentData struct {
	MarketingTitle       string
	MarketingDescription string
	Specs                []SpecRow
}

// SpecRow is one PIM attribute row.
type SpecRow struct {
	Key   string
	Value string
}

// HasContent reports whether any enrichment field is present.
func (d EnrichmentData) HasContent() bool {
	if strings.TrimSpace(d.MarketingTitle) != "" || strings.TrimSpace(d.MarketingDescription) != "" {
		return true
	}
	return len(d.Specs) > 0
}

type enrichmentFetcher struct {
	client *sdkgraphql.Client
}

func newEnrichmentFetcher(client *sdkgraphql.Client) *enrichmentFetcher {
	return &enrichmentFetcher{client: client}
}

func (f *enrichmentFetcher) Fetch(ctx context.Context, slug string) (EnrichmentData, error) {
	if f == nil || f.client == nil {
		return EnrichmentData{}, fmt.Errorf("pimdemo: graphql client not configured")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return EnrichmentData{}, fmt.Errorf("pimdemo: slug required")
	}

	var resp struct {
		Product *struct {
			MarketingTitle       string `json:"marketing_title"`
			MarketingDescription string `json:"marketing_description"`
			Specs                []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"specs"`
		} `json:"product"`
	}
	if err := f.client.Query(ctx, productEnrichmentQuery, map[string]interface{}{
		"slug": slug,
	}, &resp); err != nil {
		return EnrichmentData{}, err
	}
	if resp.Product == nil {
		return EnrichmentData{}, nil
	}

	data := EnrichmentData{
		MarketingTitle:       resp.Product.MarketingTitle,
		MarketingDescription: resp.Product.MarketingDescription,
	}
	for _, spec := range resp.Product.Specs {
		data.Specs = append(data.Specs, SpecRow{Key: spec.Key, Value: spec.Value})
	}
	return data, nil
}
