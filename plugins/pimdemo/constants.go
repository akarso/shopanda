package pimdemo

// StepName is the PDP composition step identifier.
const StepName = "pim_enrichment"

// BlockType is the UI-agnostic block type attached to PDP responses.
const BlockType = "pim_enrichment"

// productEnrichmentQuery is the mock PIM GraphQL query keyed by product slug.
const productEnrichmentQuery = `
query ProductEnrichment($slug: String!) {
  product(slug: $slug) {
    marketing_title
    marketing_description
    specs { key value }
  }
}`
