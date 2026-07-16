# pimdemo — PIM GraphQL PDP enrichment reference plugin

Demonstrates **Track E** read-path outbound integration: enrich product detail (PDP) API responses from an external PIM GraphQL API via `RegisterCompositionStep("pdp", …)`.

## Flow

1. Core PDP pipeline runs SEO, pricing, reviews, etc.
2. This plugin's `pim_enrichment` step runs last (plugin steps append after core steps).
3. Step queries mock PIM GraphQL by product slug (in-memory TTL cache, default 5m).
4. Marketing content is attached as a `pim_enrichment` block on the PDP response.

Mock PIM query:

```graphql
query ProductEnrichment($slug: String!) {
  product(slug: $slug) {
    marketing_title
    marketing_description
    specs { key value }
  }
}
```

PIM fetch failures are logged and skipped — the PDP still returns core product data.

## Enable

```yaml
plugins:
  pimdemo:
    enabled: true
    pim_graphql_endpoint: "http://localhost:9091/graphql"
    pim_api_key: optional-bearer-token
    cache_ttl: "5m"
```

Environment variables:

- `SHOPANDA_PLUGINS_PIMDEMO_ENABLED`
- `SHOPANDA_PLUGINS_PIMDEMO_PIM_GRAPHQL_ENDPOINT`
- `SHOPANDA_PLUGINS_PIMDEMO_PIM_API_KEY`
- `SHOPANDA_PLUGINS_PIMDEMO_CACHE_TTL`

## Boundaries

| Layer | Responsibility |
| --- | --- |
| Plugin | GraphQL fetch + TTL cache + composition step |
| `composition.ProductContext` | Blocks/meta on PDP responses |
| `pkg/integrationsdk/graphql` | Outbound GraphQL client |

See also: `plugins/warehousedemo` (outbound sync job), `docs/guides/PLUGIN_COMPOSITION.md`.
