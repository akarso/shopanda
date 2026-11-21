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
| PR-1556 | cart.validate `pending_line` on AddItem | done |
| PR-1557 | CartPort.DiscardEmptyGuestCart | done |
| PR-1558 | CatalogVariant.ProductVisible | done |
| PR-1559 | BoundQueue + sync request_id reinjection | done |
| PR-1560 | Product-owned register_products.go | done |
| PR-1561 | RegisterPublicRoute NoCache | done |
| PR-1562 | Product-owned root files docs + Makefile.product | done |
| PR-1596 | Product-owned path convention + PRODUCT_OWNED manifest | done |
| PR-1563 | Generic plugins.<slug> Extra config map | done |
| PR-1564 | CatalogSeedPort for plugin CLI seed | done |
| PR-1565a | Attempt-aware Complete/Fail + ErrSkipOutcome | done |
| PR-1565b | OnQueueBound / OnRequeued / Queue.Requeue | done |
| PR-1566 | CatalogSeedPort Status / Type / Attributes | done |
| PR-1567 | CatalogSeedPort EnsurePrice Update mode | done |
| PR-1568 | NoCache prefix cuts at path `{` params | done |
| PR-1569 | Plugins Extra nested map flatten + env aliases | done |
| PR-1570 | MetalDogs config via Extra (drop typed core field) | done |
| PR-1571 | Extra env empty value = unset | done |
| PR-1572 | CapCatalogSeed CLI-only (serve/worker skip) | done |
| PR-1573 | CatalogSeedPort attribute mismatch shows values | done |
| PR-1574 | Plugin CLI auto-binds CapCatalogSeed (RequiresDB) | done |
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
| Signed-in customer on plugin public routes | `CartPort.EnsureStorefrontCart` handles a signed-in customer, but routes under a prefix that skips customer JWT parsing (Metal Dogs `/api/customizer/*`, owner-token auth) never receive the identity, so the line lands in the guest cart and is not on `/cart` for a logged-in customer. Host should offer an opt-in way to pass the optional customer identity to plugin routes, without making JWT mandatory | **done (PR-1554)** — opaque → session/guest; staff Bearer defers only off admin/auth; failed JWT → 401 |
| `CatalogPort` is too narrow for hooks | `FindVariant` has `ProductID`, `FindProduct` has `Slug`: "variant belongs to product X" costs 2 queries per cart line on every `cart.validate`. Add `ProductSlug` to the variant read model or a batch lookup | **done (PR-1555)** — `ProductSlug` + `FindVariants` / `Lookup*` (Validation fallback; >100 ids errors) |
| `cart.validate` inside `AddItem` cannot see the pending line extensions | `AddItem` runs `enforceCartValidation` before the extension upsert, and `CartView` lists stored rows only. `UpsertLineExtension` needs an existing line, so a plugin cannot pre-write either. A plugin that must validate a value being added (Metal Dogs `design_id`) has to smuggle it from `cart.add_item.before` to `cart.validate` (today a goroutine-id keyed map, fragile). Expose the pending values to `cart.validate` for the line being added (e.g. `pending_line`: variant id + extension values, same per-registrant plugin-only redaction as `CartView`) | **done (PR-1556)** — overlay pending onto cart line; `pending_line` only with extensions |
| `CartPort` cannot discard an empty guest cart | A failed first add after `EnsureStorefrontCart` leaves an empty guest cart and its cookie. Add `DiscardEmptyGuestCart` or document that empty guest carts are harmless and expire | **done (PR-1557)** — `DiscardEmptyGuestCart(…, cartID)` + conditional delete; Ensure id, not request cookie |
| `CatalogPort` read scope and empty `ProductSlug` | `ProductSlug` is empty when the parent product is outside the current read scope (e.g. unpublished). A plugin that treats empty as "not my product" fails open in `cart.validate`. Document the scope for background contexts, or offer an explicit "product not visible" signal | **done (PR-1558)** — `ProductVisible` + `ClassifyProductSlug` (Match/Other/Unknown) |
| No write port for catalog seed/import | `CatalogPort` is read-only. Metal Dogs `metaldogs:catalog-seed` (products, variants, prices, price history, stock) needs write access, so the product keeps `cmd/api/metaldogs_catalog.go` building core repos in the composition root. Offer a write/seed port (or a batch import port that reuses the core importers) | **done (PR-1564)** — `CatalogSeedPort` / `CapCatalogSeed` |
| Job delivery fence (generic) | Product deviation C4: `jobs.Queue.Complete/Fail` take the dequeued attempt, `ErrSkipOutcome` skips the platform outcome write, `Requeue`/`Retry` reclaim stale processing, and the plugin installs a claim fence on the bound queue (`cmd/api/job_queue_bind.go`, `jobs_cli.go`, `internal/domain/jobs/*`). Make attempt-aware delivery, `ErrSkipOutcome` and a plugin hook on queue bind / requeue (e.g. `OnQueueBound`, `OnRequeued`) part of the platform so products need no edits in `internal/domain/jobs` or `cmd/api` | **done (PR-1565a + PR-1565b)** — attempt CAS, ErrSkipOutcome, OnQueueBound/OnRequeued, Requeue, jobs:requeue |
| Typed per-plugin config in core | `PluginsConfig` in `internal/platform/config/config.go` has one typed field per plugin (every demo, `extref`, and each product). Every product edits a core file. Provide a generic `plugins.<slug>` section (map) with env binding `SHOPANDA_PLUGINS_<SLUG>_*` and typed reads through `Host.Config*`, or a registration API that declares a plugin's config struct | **done (PR-1563 + PR-1570)** — Extra + flatten/env; typed demos kept; MetalDogs → Extra |
| Product-owned plugin registration | Products add a line to `cmd/api/register_plugins.go`, which also changes upstream. Offer a product-owned file (e.g. `cmd/api/register_products.go`, created empty by `product-init`, called from `register_plugins.go`) so the shared file never changes | **done (PR-1560)** — `register_products.go` + product-init/docs |
| Public route cache policy | A plugin's public routes cannot say "never cache" (Metal Dogs edited `wire_routes.go` and a shared prefix list for `/api/customizer`). Let `RegisterPublicRoute` take an option (e.g. `NoCache`) | **done (PR-1561)** — `pluginsdk.NoCache()` + denylist merge |
| Platform fixes found by a product | `App.BoundQueue()` (prefer `BindQueue` over `RegisterQueue` on health/read paths) and sync handler re-injecting payload `request_id` into ctx. Both carried by Metal Dogs as deviations; send upstream | **done (PR-1559)** — `BoundQueue` + sync `requestctx` reinjection |
| `CatalogSeedPort` too thin for a real product seed | PR-1564 `EnsureProduct` hard-coded `StatusActive`; seeds lacked `Type` / `Attributes`. Price history was already written by `EnsurePrice` (PR-1564). | **done (PR-1566)** — `Status` (empty → draft), `Type`, `Attributes` on product/variant; never silently active |
| `CatalogSeedPort` cannot update an existing price | `EnsurePrice` was create-only. Metal Dogs needs CSV re-seed amount changes with history. Empty `Status` on existing active is a soft no-op; Attributes stay create-only (subset match cannot add keys). | **done (PR-1567)** — `CatalogPriceSeed.Update`; history in same tx; default create-only |
| `NoCache` public route prefix ignores path params | `pathPrefixFromPattern` kept `{id}` in the denylist prefix so no URL matched. | **done (PR-1568)** — cut at `{`, trim trailing `/` |
| `Config.Extra` does not flatten nested maps | Scalars-only flatten blocked `generation.provider`; Metal Dogs carried `expandMetalDogsGeneration`. | **done (PR-1569)** — dotted flatten, list error, `.`/`_` aliases for env |
| Typed `MetalDogsPluginConfig` still in core | `internal/platform/config/config.go` keeps `Plugins.MetalDogs` (`Enabled` only) and `plugins_extra.go` lists `"metaldogs"` in `typedPluginYAMLKeys` ("migrate-next"). A typed slug blocks `Config.Extra` for it, so Metal Dogs cannot read `plugins.metaldogs.generation.*` through `Host.Config*` and carries a deviation on both files. Remove the typed field, the `typedPluginYAMLKeys` entry, the env branch for `SHOPANDA_PLUGINS_METALDOGS_ENABLED` and the `plugins.metaldogs.enabled` flatten line; the plugin reads `enabled` from `Extra` | **done (PR-1570)** — `metaldogs` → Extra; Host.Config* |
| Empty `SHOPANDA_PLUGINS_<SLUG>_<KEY>` overrides YAML | `applyPluginsExtraEnv` stores empty env values, so `SHOPANDA_PLUGINS_METALDOGS_ENABLED=""` overwrites `enabled: true` from YAML and disables the plugin. Typed plugin envs ignore empty values (`if v != ""`). Skip empty env values in `applyPluginsExtraEnv` (empty = unset) and add a test with YAML true + empty env | **done (PR-1571)** — empty env = unset |
| `CapCatalogSeed` is required in every process | A plugin that declares `CapCatalogSeed` in `Needs()` must have the port bound at every `Init` (serve, worker), although only its CLI seed command uses it. Carts already have `AssertNeedsMetExcept(CapCart)` for CLI/worker. Offer the same for write capabilities (e.g. a `NeedsInCLI` / per-context capability, or a generic `AssertNeedsMetExcept` that products can use for `CapCatalogSeed`) so serve and worker do not get catalog write access | **done (PR-1572)** — CLI-only `bindCatalogSeedPort`; serve/worker skip |
| `CatalogSeedPort` attribute mismatch error hides the values | On an existing product or variant, `catalogSeedAttributesMatch` fails without the stored and requested values, so an operator cannot see which `parent_sku` conflict they have (Metal Dogs' old seed printed both). Include key, stored value and requested value in the error | **done (PR-1573)** — key + stored + requested in error |
| No product hook to bind ports for a plugin CLI command | `runPluginCLICommand` (`cmd/api/cli_plugins.go`) builds the registry, opens the DB and runs the command, but a product cannot bind `CatalogSeedPort` for it: `registerProductPlugins(registry, cfg)` gets neither `*plugin.App` nor the DB, and PR-1572 requires a seed CLI to call `bindCatalogSeedPort(app, boot)` and `AssertNeedsMetExcept(CapCart)`. Metal Dogs carries a deviation in `cmd/api/cli_plugins.go` (+test). Either add a product-owned hook called after `InitAll` and DB open (receives command name, `*plugin.App`, `*plugin.Bootstrap`, DB) or bind `CatalogSeedPort` automatically when the plugin declared `Need(CapCatalogSeed)` and its CLI command runs. Test: a plugin CLI seed command runs end to end without a product edit of `cli_plugins.go` | **done (PR-1574)** — auto-bind on RequiresDB + CapCatalogSeed Need |
| No shared boot function per process kind | `serve`, `worker`, `scheduler`, search reindex and the plugin CLIs each call `AssertNeedsMetExcept` inline with a different capability list (`cmd/api/main.go`, `wire_services.go`, `runWorker`). A product cannot test "serve and worker boot without CLI-only capabilities (`CapCatalogSeed`, `CapCart`)" against the real boot, so Metal Dogs' test repeats the lists by hand and would not notice a change in `main.go`. Add one function per process kind that owns bind + Init + assert (e.g. `bootServe`, `bootWorker`, `bootScheduler`) and is callable from tests; products and docs reference the lists in one place. Test: a plugin with `Need(CapCatalogSeed)` boots in serve and worker and fails in a seed CLI that did not bind the port | **done (PR-1575)** — `assert*` + `bootServe`/`bootWorker` skip-list helpers (scheduler shares worker) |
| No queue API to re-pend a terminal job | `Queue.Requeue` accepts `processing` jobs only (`ErrRequeueNotProcessing`). `Admin.Retry` accepts `failed` only, resets `attempts` and does not run `OnRequeued`. A product whose fence must re-pend `failed` / `done` / `cancelled` jobs while keeping `attempts` and writing its claim fence in the same transaction has to `UPDATE jobs` itself (Metal Dogs `rependTerminalGenerateJob`, listed as a deviation in `overview.md`). Offer a terminal requeue (e.g. `Queue.Requeue` with an option or a separate `Admin.Requeue`) that keeps `attempts`, runs `OnRequeued` / `RequeueTx` in the same transaction as the re-pend, and has explicit rules for `last_error` and `run_at`. Test: failed job re-pended keeps `attempts`, hook error rolls back the re-pend | **done (PR-1576)** — `WithRequeueTerminal`; CLI `--terminal --force` (API ungated) |
| `Host.ConfigValue` cannot read core config keys | A scoped plugin host reads only `plugins.<slug>.*`. Metal Dogs must refuse `queue.driver` other than postgres at `Init` and reads `App.Config.Queue.Driver` directly. Offer a read-only, allowlisted accessor for core settings plugins legitimately depend on (e.g. `queue.driver`) or a capability that fails `Init` when the driver is unsupported | **done (PR-1577)** — `Host.PlatformConfig` + `RequireQueueDriver` |
| `TestNewJobsRuntime_WarehousedemoReachesBind` is red on main | `cmd/api/jobs_cli_test.go` builds a runtime with `warehousedemo` and an empty `media.storage`, so `core/storage-local` is enabled with an empty `media.local.base_path` and Init fails (`plugin.init.failed`: `local storage: empty base_path`). Reproduced on Shopanda `565dde5a` (a plugin init failure, not Metal Dogs wiring). Set a base path (temp dir) in the fixture or disable the core local storage there; the test logger is `io.Discard`, so print the init error on failure | **done (PR-1578)** — fixture base_path/base_url + init logs on fail |
| `TestJobQueue_Requeue_MissingID` is red against a real Postgres | `internal/infrastructure/postgres/job_queue_test.go` passes `"missing-id"` to `Queue.Requeue`; `jobs.id` is a `uuid` column, so Postgres returns `invalid input syntax for type uuid` (SQLSTATE 22P02) and the test wants `jobs.ErrRequeueNotFound` (default and `WithRequeueTerminal`). Reproduced on `de29a0c1` with `SHOPANDA_TEST_DSN`. This is also an operator-facing bug: `jobs:requeue <not-a-uuid>` surfaces a raw SQL error (Get runs before Requeue). Map non-UUID ids on Get / Requeue / Retry / Cancel (parse check + residual 22P02) to miss / not-found; MissingID uses a well-formed absent UUID | **done (PR-1579)** — Get/Requeue/transition; CLI friendly miss |
| Local storage read ignores the request context | `localfs.Storage.Read` (`internal/infrastructure/localfs/storage.go`) calls `os.ReadFile` with no context, so a cancelled request or worker job keeps reading a large blob. Metal Dogs' media store wraps `media.SignedURLStorage` and needs a context-aware read, so it edits this core file (`ReadContext`: `ctx.Err()` before and between 32 KiB chunks). Add `ReadContext(ctx, path)` to `localfs.Storage` and decide whether the `media` storage port should take a `ctx` on reads (then S3 and local both honour it). Test: cancelled ctx returns `ctx.Err()` without a full read | **done (PR-1580)** — `ReadableStorage.Read(ctx)` (invoice/wrappers); localfs chunk cancel + prealloc; local grants stay SignedURL+ServeContent |
| Flaky `TestService_ClaimGuestCart_ConcurrentSaveConflictKeepsGuestExtensions` (data race in the test fake) | `internal/application/cart/service_extensions_test.go`: `memCartExtensionValueRepo` has an unsynchronised `values` map, and two concurrent `ClaimGuestCart` calls read it (`ListTargetIDsByTypeAndIDPrefix`, line ~101) while the other writes it (`Upsert`/`UpsertBatch`, line ~58). Without `-race` it sometimes ends in `fatal error: concurrent map iteration and map write` (kills the whole package run, no `--- FAIL` line) or in `conflict: cart was modified concurrently`; with `go test -race -count=3` it fails reliably. A product CI that runs upstream packages sees random reds. Guard the fake's map with a `sync.RWMutex` (or snapshot under lock) and make the test tolerate the expected conflict outcome of the losing claim; run the package with `-race` in CI | **done (PR-1581)** — RWMutex fakes + barrier order; both claims succeed via MergedGuestID short-circuit; `unit.sh` `-race` cart |
| No public jobs port for plugins | A non-core plugin may import only `pkg/*`, but the SDK exposes jobs only as `ErrSkipOutcome`, `RequeueTx`, `OnQueueBound` and `OnRequeued`. Metal Dogs imports `internal/domain/jobs` (`Queue`, `Reader`, `StatusPending/Processing/Failed/Done/Cancelled`, `ErrRequeue*`, `WithRequeueMinAge`, `WithRequeueTerminal`) to read job state (get, counts by status), requeue and render health (3 production files). Add a `Host.Jobs()` port (e.g. `CapJobs`: `Get`, `CountsByStatus`, `Requeue(ctx, id, attempt, opts...)`) and re-export the status constants, requeue options and errors from `pkg/` | **done (PR-1584)** — `CapJobs` + `Host.Jobs()` |
| No public media storage port for plugins | A plugin that stores or serves private blobs needs the `media` storage types (`Storage`, `SignedURLStorage`, `ReadableStorage`, `SignURLOptions`, dispositions, `AsSignedURLStorage`). Metal Dogs imports `internal/domain/media` in `assets.go` and `adapters/mediastore` to wrap the registered storage (key rejection, content type, ctx read). Add `Host.Media()` / `CapMedia` or re-export the interfaces from `pkg/`; consider moving the generic hardening into core (see context-aware read, PR-1580) | **done (PR-1585)** — `CapMedia` + `Host.Media()` |
| No request id accessor in `pkg/` | `internal/platform/requestctx.RequestID` is the only way to read the request id from a context, so plugin log correlation (Metal Dogs `observability/correlation.go`) imports `internal/*`. Add `pluginsdk.RequestIDFromContext(ctx)` (and keep the sync-job reinjection from PR-1559) | **done (PR-1582)** — `RequestIDFromContext` read-only |
| No money/currency helper in `pkg/` | Plugins parse human prices into minor units and validate ISO currencies themselves (Metal Dogs `parseNetMinor` and a copy of `shared.IsValidCurrency` from `internal/domain/shared`). Add a `pkg/` helper: decimal string → minor units without rounding ("80,49", "1.234,56", "1,234.56"; rejects spaces, letters, extra decimals, negatives, zero, empty) taking the currency exponent (JPY 0, KWD 3), plus `IsValidCurrency` | **done (PR-1583)** — `extapi.ParseMinorUnits` + `IsValidCurrency` |
| No public test database helper in the harness | Plugin tests need an isolated Postgres schema (search_path pinned on every pool connection, dropped on cleanup) with core migrations and the plugin's migrations applied; Metal Dogs `testutil.OpenIsolated` imports `internal/platform/migrate` (`Run`, `RunPluginFS`) for it. Add the helper to `pkg/pluginsdk/harness` (skips when `SHOPANDA_TEST_DSN` is unset) | **done (PR-1586)** — `harness.OpenIsolated` + AdaptInit DB |
| Plugin still needs `internal/platform/plugin` for sync jobs and `App` | Metal Dogs imports `internal/platform/plugin` in 10 production files (`App`, `SyncJobRegistration`, `NewSyncJobEnvelope`, `InjectRequestID`, `MigrationSpec`, `Migrator`, `Capability`/`Cap*`). `pluginsdk` already has `SyncJobs`, `Migrator`, `Capability`. Document the mapping per symbol and confirm `SyncJobs.RegisterManual`/`Enqueue` cover request-id injection and the envelope; fill whatever is missing so a plugin can drop the `plugin.App` import entirely (Host + `Adapt`) | **done (PR-1587)** — symbol map + `InjectRequestID` |
| `pluginsdk.Migrator` is not recognised for a `plugin.App` plugin | `Registry.CollectMigrations` and `migratorRegistration` type-assert `plugin.Migrator` (`Migration() plugin.MigrationSpec`). `pluginsdk.Migrator` returns `pluginsdk.MigrationSpec`, a different named type; only `pluginsdk.Adapt` forwards it to the core contract (`migrator.go`), so a legacy `plugin.App` plugin that implements only the SDK interface is silently skipped (no migration applied, no error). Metal Dogs keeps `plugin.Migrator` and an `internal/platform/plugin` import in `migrations.go` for it. Make the two types the same (type alias `pluginsdk.MigrationSpec = plugin.MigrationSpec`) or make the registry accept both interfaces; add a test: a `plugin.App` plugin implementing only `pluginsdk.Migrator` has its migration collected and applied | **done (PR-1589)** — `MigrationSpec` type alias |
| `SyncJobs.Enqueue` cannot set the job id, so idempotent enqueue has no SDK form | Metal Dogs `enqueueGenerateIdempotent` / `enqueuePrepareProduction` (`plugins/metaldogs/sync_generate.go`, `sync_prepare_production.go`) set `job.ID` to the generation job id, call `Queue.Enqueue`, and on a conflict `Get` + `Requeue` the existing job. `pluginsdk.SyncJobs.Enqueue(ctx, name, payload)` builds the envelope with a generated id; `JobsPort` has `Get`/`CountsByStatus`/`Requeue` but no `Enqueue`. Without a way to pick the id a plugin on `pluginsdk.Plugin` cannot keep one-job-per-entity semantics and must keep `plugin.App.BoundQueue()`. | **done (PR-1592)** — `EnqueueOption` `WithJobID` + Conflict unwrapped for `IsConflict`; Get/Requeue resume test |
| `JobsPort` has no per-type counts | Metal Dogs admin health (`plugins/metaldogs/httpapi/health.go`) reports pending/processing/failed per registered job type via `jobs.Reader.CountsByType(ctx, jobType)`. `pluginsdk.JobsPort` exposes `Get`, `CountsByStatus` (all types together) and `Requeue`, so a plugin on `pluginsdk.Plugin` cannot build the per-type table and would have to keep `plugin.App.BoundQueue()`. | **done (PR-1593)** — `JobsPort.CountsByType` API landed (empty-type validate + two-type test); Metal Dogs health consumer migration still pending |
| Plugin CLI asserts `CapJobs`/`CapMedia` needs it never binds | `Host.Jobs()`/`Host.Media()` return a port only when the plugin declared `CapJobs`/`CapMedia` (`pkg/pluginsdk/apphost.go`), so a plugin on `pluginsdk.Plugin` must `Require` them. `runPluginCLICommand` (`cmd/api/cli_plugins.go`) opens the DB and binds only `CatalogSeed`; `pluginCLINeedsSkip` (`cmd/api/plugin_boot.go`) skips `CapCart`, `CapIntegration*` and (unbound) `CapCatalogSeed` but not `CapJobs`/`CapMedia`, so `assertPluginCLINeedsMet` fails and every CLI command of that plugin (Metal Dogs `metaldogs:templates`, `metaldogs:catalog-seed`) is refused. | **done (PR-1594)** — skip CapJobs/CapMedia in `pluginCLINeedsSkip` (CLI binds neither) |
| `WithJobID` conflict resolve does not retry a raced requeue | `SyncJobs.Enqueue(..., WithJobID(id))` on a terminal job calls `Requeue(WithRequeueTerminal)` once. Two concurrent regenerate calls: Postgres loser typically gets `ErrRequeueNotTerminal` (row already non-terminal); stale attempt count yields `ErrRequeueWrongAttempt`. Without a retry, Enqueue returned a hard error and Metal Dogs kept its own WrongAttempt/NotTerminal wrapper. | **done (PR-1595)** — NotTerminal=success; WrongAttempt reload+retry once (second WrongAttempt hard-fails; best-effort) |
| `WithJobID` always requeues a terminal job, no way to ask for conflict only | Metal Dogs prepare-production keeps failed/done rows as they are (only cancelled and stale processing are reset by the plugin). `SyncJobs.Enqueue(..., WithJobID)` on a terminal row calls `Requeue(WithRequeueTerminal)`. The plugin checks `Host.Jobs().Get` before enqueue, but the check is not atomic: a row that is absent at `Get` and terminal when `Enqueue` runs is requeued against the rule (documented in Metal Dogs CR-26; impact small while the handler is `pending_spec`). | Add an enqueue option (e.g. `WithJobIDConflictOnly()` or `WithTerminalPolicy(Keep|Requeue)`) so a duplicate id returns `IsConflict` instead of requeueing; default stays requeue. Test: terminal row + option → `IsConflict`, row unchanged. Not blocking Metal Dogs MIG3b. |
| No single convention for product-owned files outside `plugins/<slug>` | Metal Dogs edits 13 upstream-owned root files to hang product behaviour on them: `Makefile` (docs-policy target), `docker-compose.yml` (Postgres on 127.0.0.1, `SHOPANDA_DATABASE_HOST_PORT`), `.env.example`, `configs/config.example.yaml`, `.gitignore`, `scripts/ci/docs-policy.sh` and its seed copy `scripts/product-init/seed/scripts-ci/docs-policy.sh` (product link checker), `scripts/product-init/product-init_test.sh` and `reserved-slugs.txt` (reserved product slug), `.github/scripts/run-integration-tests.sh`, `.github/scripts/check-product-docs-links.sh`, `.cursor/rules/pr-documentation-checklist.mdc`, plus the product-owned `cmd/api/register_products.go` (PR-1560). Only `register_products.go` (merge=ours) and `Makefile.product` (PR-1562) have a defined extension mechanism today; every other file is an ad hoc fork point, so each product invents its own place and the next bump conflicts there. Goal is one coherent convention, not a smaller diff: a developer should find product files and config in predictable places. | **done (PR-1596)** — convention + Make/docs-policy/compose/gitignore/reserved-slugs hooks; env/config = doc companions; `.github/scripts` / `.cursor/rules` still open |
| Plugin-level `Need(CapCatalogSeed)` breaks the plugin's non-DB CLI commands | After PR-1574, `runPluginCLICommand` (`cmd/api/cli_plugins.go`) refuses any command of a plugin that declared `Need(CapCatalogSeed)` when that command has `RequiresDB` false (the port is bound only for `RequiresDB` commands of the owning plugin). A plugin with one seed command and other commands that need no database (Metal Dogs `metaldogs:eval`) cannot run the latter from the real CLI; tests that call `cmd.Run` directly do not notice. Make the need per command (e.g. `CLICommand.Needs`) or bind `CatalogSeedPort` only when the invoked command asks for it and let other commands of the plugin run; add a test with one plugin, one seed command and one `RequiresDB: false` command through `runPluginCLICommand` | **done (PR-1590)** — `CLICommand.Needs` + sibling non-DB OK |
| CapCatalogSeed CLI docs / Needs scope after PR-1590 | EXTENSION_POINTS still said plugin-level CapCatalogSeed + RequiresDB:false fails closed; `CLICommand.Needs` looked like a general Cap API; command Needs alone could bind without plugin declare. Tighten: CapCatalogSeed-only Needs (unknown fail closed); bind only when `CapabilityDeclaredBy` **and** (command Needs seed or empty-Needs compat); update living guides + tests (compat over-bind, no-declare Host false, InitAll apply for SDK Migrator) | **done (PR-1591)** — CR fixes for 1589/1590 |
| Product-owned root files conflict on every bump | `README`, `AGENTS`, `RUNBOOK`, `PLUGINS`, `.gitignore`, `Makefile`, CI workflows differ in every product fork. Document `.gitattributes` `merge=ours` for product-owned files and a `Makefile` `-include Makefile.product` hook; keep product targets out of the shared Makefile | **done (PR-1562)** — PRODUCT_SITES + `-include Makefile.product` |

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
| 9 | cart.validate `pending_line` on AddItem | **1556** |
| 10 | CartPort.DiscardEmptyGuestCart | **1557** |
| 11 | CatalogVariant.ProductVisible | **1558** |

Pitfalls: hook visibility (1548), trusted-write docs (1542/1550), merge/claim docs (1550), atomic Save (1549), CLI/worker ports (1547), session cart / owner scoping (1553), owner-token + session identity (1554), catalog slug batch (1555), pending_line (1556), empty guest discard (1557), ProductVisible (1558).
