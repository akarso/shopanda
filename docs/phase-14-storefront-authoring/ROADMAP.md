# Phase 14 — Storefront authoring roadmap

## Tracks

| Track | Goal | PR range |
| --- | --- | --- |
| **A** | SSR packaging | PR-1100–1105 |
| **B** | Headless API | PR-1106–1110 |
| **C** | Storefront JS seam (follow-up) | PR-1111–1112 |

```text
1100 → 1101 → 1102 → 1103 → 1104 → 1105
1106 → 1107 → 1108 → 1109 → 1110
1111 → 1112
```

Tracks A/B are independent except that docs (1105 / 1110) should land after their code. Track C is a follow-up after 1110.

## Track C — Storefront JS seam (follow-up)

Same-origin configurator scripts that must land on SSR `/cart` / `/checkout`. Does **not** merge the headless REST cart with the cookie cart.

| PR | Title | Status |
| --- | --- | --- |
| PR-1111 | `POST /storefront/cart/items` + `GET /storefront/csrf` | done |
| PR-1112 | JSON update/remove on the same cookie cart | done |

## Track A — SSR packaging

| PR | Title | Status |
| --- | --- | --- |
| PR-1100 | Vendor pinned htmx 1.9.12 | done |
| PR-1101 | Static inheritance + cache-bust | done |
| PR-1102 | Dev-mode theme watch | done |
| PR-1103 | `theme:init` + `_starter` | done |
| PR-1104 | `frontend.mode: headless` honors skip-HTML | done |
| PR-1105 | Spec sync (theme/frontend/templating) | done |

## Track B — Headless

| PR | Title | Status |
| --- | --- | --- |
| PR-1106 | CORS allowlist | done |
| PR-1107 | Product/category by slug + opt-in snake_case JSON | done |
| PR-1108 | Address-book JSON + claim routes always on | done |
| PR-1109 | OpenAPI catch-up | done |
| PR-1110 | `examples/headless-vanilla` + HEADLESS.md | done |

## Out of scope

- New template language
- Official Next/React storefront
- Alpine in core
- Embedding themes in the binary
- Magento layout XML
- Hybrid mode (cross-origin). Same-origin JS → SSR cart is Track C.
