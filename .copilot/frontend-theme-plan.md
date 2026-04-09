# Frontend Base Theme — Future Milestone

## Trigger
Start after Phase 23 (PR-085: CMS pages) lands. Prerequisites: theme engine (Phase 17), seed data (Phase 19), URL routing + CMS (Phase 23).

## Concept
"Shopanda Base" — a default theme shipped as a core plugin (like Magento's Luma). Separate roadmap (`FRONTEND_ROADMAP.md`), not inline in main ROADMAP.md.

## Estimated Scope: 12–16 PRs

### Foundation (2–3 PRs)
- Base layout, CSS framework (Tailwind or vanilla), asset pipeline
- Navigation component (category tree, search bar, account links)

### Catalog Pages (2–3 PRs)
- PLP: category listing + search results (consumes composition pipeline)
- PDP: product detail page with variants, pricing, add-to-cart
- Collection page

### Commerce Pages (3–4 PRs)
- Cart page (item list, quantity update, remove, totals)
- Multi-step checkout (shipping → payment → confirm)
- Order confirmation page
- Order history / detail

### Auth & Account (2 PRs)
- Login / register pages
- Account dashboard + profile edit

### Content (1–2 PRs)
- Home page (featured products, categories, CMS blocks)
- CMS page template, 404/500 error pages

### Polish (1–2 PRs)
- Responsive pass
- SEO integration (meta tags, JSON-LD, breadcrumbs)

## Key Decisions (to resolve when starting)
- CSS strategy: Tailwind vs vanilla CSS vs other
- JS strategy: minimal vanilla JS vs Alpine.js vs htmx
- Image handling: srcset, lazy loading, CDN integration
- Whether admin UI gets the same theme or stays API-only
