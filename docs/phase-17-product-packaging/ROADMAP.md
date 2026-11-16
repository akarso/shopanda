# Phase 17 — Product packaging & consumer CI

## Strategy

- Extract the **portable quality contract** from GitHub YAML into `scripts/ci/*`.
- Keep live Actions on this upstream repo; ship **samples only** for product consumers.
- Add `product-init` scaffold + docs so a product site is drop-in: custom plugin work + optional settings.
- Design (do not fully land) a public extension SDK so products can eventually leave the fork.
- One PR = one responsibility; PR specs under `prs/` (**PR-1500–1555**). Each PR is **`[oss]`**.

---

## What ships, by subsystem

| Subsystem | Today | End of Tracks A–D |
| --- | --- | --- |
| Quality checks | Inline in `.github/workflows/ci.yml` + `.github/scripts/*` | `scripts/ci/*`; workflows call scripts only |
| Product CI | Inherit Shopanda workflows or break | Opt-in `ci-samples/` + runbook; local/FTP first-class |
| Release image | Hard-coded `ghcr.io/<owner>/shopanda` | `SHOPANDA_IMAGE_NAME` (default `shopanda`) |
| `.gitignore` | Bare `src` | Path-scoped; does not ignore `storefront/src` |
| Product bootstrap | Hand-copy `plugins/example` | `scripts/product-init.sh` + Phase 13 skeleton |
| Job type docs | Easy to invent free-form names | Canonical `integration.sync.{slug}.{name}` only |
| Public SDK | Fork forever for `internal/` | ADR + Track E plan (implement after A–D) |

---

## Tracks

| Track | Goal | PR range | Site-ready? |
| --- | --- | --- | --- |
| **A** | Portable quality contract | PR-1500–1505 | Required |
| **B** | Consumer packaging & sample CI | PR-1510–1516 | Required |
| **C** | Product scaffold | PR-1520–1526 | Required |
| **D** | Extension contract clarity | PR-1530–1534 | Required |
| **E** | Public extension SDK | PR-1540+ | Later (after A–D) |

```text
1500 → 1501 → 1502 → 1503 → 1504 → 1505     (Track A)
                │
                └──> 1510 → … → 1516         (Track B)
                              │
                              └──> 1520 → … → 1526   (Track C)
                                            │
                                            └──> 1530 → … → 1534   (Track D)
                                                          │
                                                          └──> 1540+   (Track E, later)
```

---

## Site-ready gate

Do **not** expect drop-in product repos until Tracks **A–D** are `done`:

- `./scripts/ci/unit.sh` matches what platform CI runs for unit checks
- `product-init` yields a buildable plugin + flag + health route pattern
- Sample CI is opt-in; omitting it is documented and supported
- Image name cannot clobber `shopanda` GHCR by accident
- Job naming docs match Phase 13 (`integration.sync.{slug}.{name}`)
- `.gitignore` does not ignore `storefront/src`

Track E is **not** required to start a product fork; it removes the fork tax later.

---

## Track A — Portable quality contract (PR-1500–1505)

| PR | Title | Status |
| --- | --- | --- |
| PR-1500 | `scripts/ci/` layout | done |
| PR-1501 | Rewire `ci.yml` to call scripts only | done |
| PR-1502 | `Makefile` + human entrypoint; no git hooks | done |
| PR-1503 | Configurable govuln baseline path | done |
| PR-1504 | Docs-policy skips missing optional paths | done |
| PR-1505 | Guide: run the same checks CI runs | done |

---

## Track B — Consumer packaging & sample CI (PR-1510–1516)

| PR | Title | Status |
| --- | --- | --- |
| PR-1510 | Spec: three consumer modes | done |
| PR-1511 | `ci-samples/github` + `ci-samples/gitlab` | done |
| PR-1512 | Runbook: enable samples or omit for FTP/local | done |
| PR-1513 | `SHOPANDA_IMAGE_NAME` for release | done |
| PR-1514 | Fix `.gitignore` bare `src` | done |
| PR-1515 | Fetch-only `upstream` docs | done |
| PR-1516 | Platform vs product docs split | done |

---

## Track C — Product scaffold (PR-1520–1526)

| PR | Title | Status |
| --- | --- | --- |
| PR-1520 | `scripts/product-init.sh` | done |
| PR-1521 | Phase 13 plugin skeleton (Migrator + health) | done |
| PR-1522 | `--with-ci-samples` copies scripts + samples | done |
| PR-1523 | GOVULN baseline stub template | done |
| PR-1524 | Product README template | done |
| PR-1525 | Walkthrough via generated skeleton / example | done |
| PR-1526 | DEVELOPER: building a product (primary path) | done |

---

## Track D — Extension contract clarity (PR-1530–1534)

| PR | Title | Status |
| --- | --- | --- |
| PR-1530 | Canonical job-type naming in product docs | done |
| PR-1531 | Contract golden docs for Migrator + RegisterManual | done |
| PR-1532 | Upstream deviations template | done |
| PR-1533 | Product-fork upgrade runbook | done |
| PR-1534 | Free-form job types remain out of scope | done |

---

## Track E — Public extension SDK + extension ergonomics (PR-1540–1555)

| PR | Title | Status |
| --- | --- | --- |
| PR-1540 | ADR accepted + import inventory | done |
| PR-1541 | `pluginsdk.Host` facade | done |
| PR-1542 | Public Money, FieldDef, PluginWrite | done |
| PR-1543 | Public pricing step contracts | done |
| PR-1544 | Public checkout + composition contracts | done |
| PR-1545 | Cart / catalog / order ports on Host | done |
| PR-1546 | Declarative Needs | done |
| PR-1547 | Unified InitAll port wiring | done |
| PR-1548 | Hook sees extensions + auth | done |
| PR-1549 | Atomic cart extension + Save | done |
| PR-1550 | Extension-point catalog + AGENTS recipes | done |
| PR-1551 | Reference plugin + harness | done |
| PR-1552 | Migrate demos; ImportBoundary pkg-only | done |
| PR-1553 | CartPort session cart + validate extension visibility | done |
| PR-1554 | Optional customer identity on plugin public routes | done |
| PR-1555 | CatalogPort ProductSlug + FindVariants | done |
| E2–E4 | External module publish + fork→module guide | later |

See [`specs/PUBLIC_EXTENSION_SDK.md`](specs/PUBLIC_EXTENSION_SDK.md).

---

## Explicitly out of scope

| Deferred | Why not now |
| --- | --- |
| Removing CI from `akarso/shopanda` | Undoes Phase 10 trust for the engine |
| Mandatory git hooks | Products must be able to FTP / local-only without friction |
| Dynamic `.so` / marketplace | Unchanged deferred list |
| Free-form custom job types | Phase 13 deferral stands (PR-1534) |
| Metal Dogs product work | Consumes this phase after pin bump |
| Phase 15/16 merchant/visual work | Independent |

---

## Validation target (phase, not per-PR)

- `make check` / `./scripts/ci/unit.sh` passes on a clean tree the same way `CI / unit` does (modulo Node for admin UI tests).
- `./scripts/ci/docs-policy.sh` succeeds when optional docs paths are absent (skips missing roots).
- `SHOPANDA_IMAGE_NAME=myproduct` changes release image coordinates in workflow meta.
- `bash scripts/product-init.sh --slug acme --with-ci-samples` creates `plugins/acme/` and refreshes `scripts/ci/` + `ci-samples/` from seed; without the flag, samples are untouched; neither mode installs live workflows.
- `bash scripts/product-init/product-init_test.sh` and `bash scripts/ci/docs-policy_test.sh` pass (`make phase17-scripts-test`).
- Docs state job types as `integration.sync.{slug}.{name}` only.
- `git check-ignore -v storefront/src/main.js` reports not ignored.

---

## Follow-ups from product consumers

| Item | Detail | Tracking |
| --- | --- | --- |
| Cart extension write is not atomic with `carts.Save` | Save-then-upsert + compensate (AddItem); merge CopyTarget after Save | **done (PR-1549)** |
| `CartPort` covers only `AddItem` | Metal Dogs E2 also needs: get/create the session cart (replace a non-active one), read lines with extension values (same design on same variant = no-op, other design = 409), remove a stale plugin-only value after a failed add. Trusted-write context must stay limited to the call that writes. Without this a product plugin still needs its own port in `cmd/api` | **done (PR-1553)** — `Get` / `EnsureActive` / `EnsureStorefrontCart` / `DeleteLineExtension` |
| Hook line view has no extension values | `extapi.CartLineView` has only `VariantID`, `Quantity`. A `cart.validate` handler cannot see a plugin-only line value (e.g. `metaldogs.design_id`). Add values to the view or an `ExtensionReader` on `Host` | **done (PR-1553)** — `CartLineView.Extensions` + `cart.validate` `InvokeAdapted` owner redaction |
| Reference for design-on-line pattern | Second `extref` example: plugin-only line field, add route with idempotency/conflict, `cart.validate` blocking issue, fail-closed when a dependency is unwired | **done (PR-1553)** — `extref.design_id` + `POST /extref/design-line` |
| Public route and the guest session cart | Document (or provide a port for) how a plugin `RegisterPublicRoute` handler reaches the storefront session cart without exposing a raw cart id | **done (PR-1553)** — `pkg/storefrontsession` + `CartPort.EnsureStorefrontCart` (keep `cart_id` out of JSON) |
| Signed-in customer on plugin public routes | `CartPort.EnsureStorefrontCart` handles a signed-in customer, but routes under a prefix that skips customer JWT parsing (Metal Dogs `/api/customizer/*`, owner-token auth) never receive the identity, so the line lands in the guest cart and is not on `/cart` for a logged-in customer. Host should offer an opt-in way to pass the optional customer identity to plugin routes, without making JWT mandatory | **done (PR-1554)** — AuthMiddleware: failed/non-customer Bearer falls back to session cookie; non-Bearer Authorization does not 401; `pluginsdk.CustomerID` |
| `CatalogPort` is too narrow for hooks | `FindVariant` has `ProductID`, `FindProduct` has `Slug`: "variant belongs to product X" costs 2 queries per cart line on every `cart.validate`. Add `ProductSlug` to the variant read model or a batch lookup | **done (PR-1555)** — `CatalogVariant.ProductSlug` + `CatalogPort.FindVariants` (batch via FindByIDs/ListByIDs when available) |

---

## Extension ergonomics (Track E input → PR-1541–1555)

Goal: a plugin author follows a convention and does not need to read the whole codebase. Evidence: Metal Dogs E1 and E2.

| # | Proposal | PR |
| --- | --- | --- |
| 1 | Stable public SDK; plugins never import `internal/*` | 1541–1545, 1552 |
| 2 | Declarative Needs | 1546–1547 |
| 3 | Extension-point catalog + pitfalls | 1550 |
| 4 | Reference plugin + harness | 1551 |
| 5 | Recipes in `AGENTS.md` | 1550 |
| 6 | CartPort session cart + validate extension visibility | **1553** |
| 7 | Optional customer identity on plugin public routes | **1554** |
| 8 | CatalogPort ProductSlug + FindVariants | **1555** |

Pitfalls: hook visibility (1548), trusted-write docs (1542/1550), merge/claim docs (1550), atomic Save (1549), CLI/worker ports (1547), session cart / owner scoping (1553), owner-token + session identity (1554), catalog slug batch (1555).
