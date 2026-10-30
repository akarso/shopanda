# Runbook

The operational guides live in [`docs/guides/`](docs/guides/):

- [**Deployment Guide**](docs/guides/DEPLOYMENT.md) — install, configure, run, scale, back up, and troubleshoot Shopanda
- [**Developer Guide**](docs/guides/DEVELOPER.md) — extend the platform via plugins, events, pipelines, and workflows
- [**Extension Points**](docs/guides/EXTENSION_POINTS.md) — which mechanism (events, hooks, pricing pipeline, checkout workflow, composition steps, registries) to reach for
- [**Merchant Guide**](docs/guides/MERCHANT.md) — manage products, orders, and day-to-day store operations

## Incident response

Symptom → check → fix, for the scenarios most likely to page someone. Each links back to the fuller reference section below it for detail this summary skips.

### Database unreachable

**Symptom:**
- At startup: `serve`/`worker` exits immediately, stderr `error: database: db: ping: <driver error>`. Both processes behave identically — neither retries, neither starts in a degraded mode.
- Mid-run: `GET /readyz` returns `503 {"status":"unavailable"}` while `GET /healthz` stays `200` — the process is alive but can't reach Postgres. Regular API requests on DB-backed routes return `5xx` with no single unifying log line; check `http.request` log entries for `status >= 500` on the affected routes.
- Worker process: repeating `worker.dequeue.failed` log lines, one per poll tick — the worker does **not** crash, it keeps polling indefinitely.

**Check:**
```bash
curl -s -o /dev/null -w '%{http_code}\n' localhost:PORT/healthz   # expect 200 — rules out a crashed process
curl -s -o /dev/null -w '%{http_code}\n' localhost:PORT/readyz    # 503 confirms DB unreachable from this process
pg_isready -h <host> -p <port>                                    # or: psql "$DATABASE_URL" -c 'select 1'
```

**Fix:** restore Postgres connectivity (network/credentials/DB itself). If the DB was down at process **start**, the process already exited and needs a manual/orchestrator restart once connectivity is confirmed. If it dropped **mid-run**, both `/readyz` and the worker self-heal on the next successful ping/dequeue — no restart needed. See ["Liveness vs readiness"](#liveness-vs-readiness) below for the full probe contract.

### Bad migration

**Symptom:** `./shopanda migrate` exits non-zero: `migrate <file>: exec: <pg error>` or `migrate <file>: record version: <pg error>`.

**Check:**
```bash
psql "$DATABASE_URL" -c "select version from schema_migrations order by version desc limit 5;"
```
The failing file is **not** in this table — each migration runs inside one transaction (`BEGIN` … exec … record version … `COMMIT`), so a failure rolls back cleanly and leaves the schema exactly as it was before that file ran.

**Fix:** fix the `.sql` file (or author a new corrective migration with the next numeric prefix — never renumber or edit an already-shipped one), then re-run `./shopanda migrate`. It only (re)attempts files not yet recorded, so retrying is safe. Exception: a statement that cannot run inside a transaction block (e.g. `CREATE INDEX CONCURRENTLY`) fails differently — Postgres may leave a partial object behind; inspect manually (`\d <table>`, `pg_stat_progress_create_index`) rather than assuming the same clean rollback applies.

There is **no down-migration mechanism** in this codebase. "Rollback" for a schema change means restoring from backup, not reversing a migration file — see [DEPLOYMENT.md's backup/restore commands](docs/guides/DEPLOYMENT.md) (`pg_dump` / `psql`).

### Rollback (deploy / migration / plugin config)

What actually exists today, so you don't go looking for tooling that isn't there:

| What broke | Rollback path | What does **not** exist |
| --- | --- | --- |
| Bad container image / deploy | Manually redeploy the previous known-good `sha-<commit>` / digest pin | No scripted rollback command anywhere in this repo — **keep a record of the last-known-good tag before every deploy**, nothing here tracks deploy history for you |
| Bad migration | Restore from backup (`pg_dump`/`psql`, see [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md)) | No down-migrations, no `migrate down` |
| Bad plugin config | Revert the config flag/env var, then restart | No runtime plugin disable/enable API. `serve` degrades and keeps running (`plugin.init.failed` logged, other plugins unaffected); `worker`/`scheduler`/`search:reindex` instead **exit non-zero** on the same failure — either way, a restart is required after fixing the config |

### Webhook SSRF rejects

**Symptom:** admin create/update of a webhook endpoint returns a validation error `ssrf: destination address ... is not allowed` (or `ssrf: non-canonical IP host is not allowed`); or a delivery job repeatedly fails with `worker.job.failed` whose error contains `ssrf: destination resolves to disallowed address ...` — the domain looks fine but its DNS record currently resolves into a blocked range.

**Check:**
```bash
dig +short <endpoint-host>   # what does it resolve to right now?
```
Compare against the blocked ranges in ["Outbound webhooks (SSRF)"](#outbound-webhooks-ssrf) below (private/link-local/reserved/NAT64). Also confirm the endpoint is `https://` — a plain `http://` URL is rejected regardless of IP and can look like an SSRF false positive at a glance.

**Fix:** if the destination is genuinely private/reserved, this is the control working as intended — no fix needed. If it's a legitimate public endpoint that happens to resolve into a blocked range, **there is currently no allowlist or override** — onboarding it requires a code change to `internal/platform/ssrf`, not a config flag. Do not attempt to route around this check; escalate to engineering.

### Rate-limit / login lockouts

**Symptom:** a specific user gets `429` / `too many login attempts, try again later` on login; or general API clients get `429` / `rate_limited`.

**Check:** these are two different limiters — distinguish which one fired. Login lockout is keyed by **IP + normalized email** (`auth.lockout`); a single user locked out on login while their other API calls succeed is lockout, not the general HTTP rate limit. Confirm configured thresholds: `auth.lockout.max_failures` (default 10), `auth.lockout.window` (default 15m), `auth.lockout.store` (`cache` default, shared across instances; `memory` is single-instance only — logs `auth.lockout.store_memory` at startup if selected).

**Fix:** **there is no admin unlock endpoint or CLI command today.** The remediation paths that actually exist:
- Wait it out — lockout self-clears after one full quiet window (default 15 minutes) with no further failed attempts.
- `store=memory` only: restarting the affected instance clears lockout state — but for **every** locked-out key on that instance, not just the one you meant to clear.
- `store=cache`: technically clearable by deleting the backend cache key directly (`auth:lockout:` + SHA-256 of `"<ip>|<lowercased, trimmed email>"` — the email **must** be lowercased and trimmed exactly as the app does before hashing, or the deletion silently targets the wrong key: no error, key just isn't found, and the user stays locked out with no indication anything went wrong). This needs direct cache access and manually computing the hash — no supported tool does this for you, and this is not a safe first move under incident pressure. Prefer waiting it out (default 15 minutes) unless the situation genuinely can't wait.

For general `429`/`rate_limited` (not login-specific): with `rate_limit.driver=memory` (default) this is per-process and not shared across instances; `rate_limit.driver=redis` shares a sliding window of burst/rate seconds (capacity = burst) across instances. Redis errors default to fail-open (`rate_limit.on_error=open`) so an outage does not 429 the fleet — watch `shopanda_ratelimit_backend_errors_total`. If a shared NAT/proxy is causing false positives, set `rate_limit.trusted_proxies` so `ClientIP` is the rightmost untrusted hop — see ["Rate limiting and login lockout"](#rate-limiting-and-login-lockout) below.

### A permission-catalog or category-nav change isn't taking effect on some instances

**Symptom:** the admin roles editor's list of *assignable permissions* (not a role's actual grants — see below), or the storefront nav, shows the OLD data on some requests but not others. The category-nav case never lasts longer than about 45 seconds (its TTL). The permission-catalog case is different — see its own bullet below, it can persist well past 30 seconds if the cause is a plugin upgrade.

**Check:** this is PR-1040's L1 (in-process) cache, not a write failure — a write that actually failed would show the old data forever, not for a bounded window. This does **not** apply to granting/revoking a role's permissions: `UpdateRole`/`GetRole`/`ListRoles` read straight from the DB, uncached, so a grant/revoke is visible on the very next request everywhere — if that looks stale, it's a different bug, not this one. Confirm which L1 use site is actually involved:
- Permission catalog (`adminrole.Service.Catalog()`, the roles editor's "assignable permissions" list — the permission *definitions*, not any role's assignments) — 30s TTL, but that TTL is **not** the actual staleness bound here: `rbac.Registry` (what it caches) is frozen after plugin `Init()` and never mutates again, so a stale catalog means a **new plugin version** shipped a permission and the OLD process just hasn't restarted yet — every TTL expiry just recomputes the SAME stale, pre-upgrade catalog. The real fix is restarting/redeploying that instance, not waiting; check `plugin.init.summary` in that instance's own startup logs to confirm.
- Category tree (`storefront.StorefrontHandler`'s nav) — 45s TTL, same as before PR-1040, and this one genuinely does bound the staleness (the underlying data actually changes at runtime). A category create/update/delete evicts this **immediately, but only in the process that handled that admin request** — see below for why.

**Fix:** HTTP `POST /api/v1/admin/cache/clear` with `{"all": true}` clears L1 only in the API process that handled the request. Other replicas keep their L1 until an in-process invalidation event or TTL expiry. The CLI cannot clear an API process's L1 (`app cache:clear --all` is a separate process: it flushes L2 only and has no L1 of its own). Two things remain true, and neither is a bug:
- **Within the process that made the change**, the category tree cache evicts immediately (subscribed directly to the category create/update/delete events) — if you're still seeing stale nav from that SAME instance past the eviction, it's not L1; look elsewhere.
- **On every OTHER instance/replica**, only the TTL above bounds the staleness unless you also POST `{"all": true}` to that replica — `internal/platform/event.Bus` is in-process only (no Redis pub/sub, no network I/O; every replica runs its own independent instance), so the eviction signal from one instance's write never reaches another instance's memory. Waiting out the TTL (worst case ~45s) is the usual remediation; a full-clear on one replica does not flush the others. See `docs/guides/DEVELOPER.md`'s "When it's safe to use the L1 (in-process) cache" for the full design reasoning.
- The permission catalog case above is different: there is no write to wait out, so don't wait — restart/redeploy the stale instance.

### A shopper reports seeing someone else's data

**Symptom:** a storefront page (PDP, category, search, CMS, home) shows another customer's name, cart, order, or a CSRF token that isn't theirs. Treat this as the same severity class as SSRF/lockout — shared HTML was served from the full-page cache (PR-1044).

**Check:**
- Confirm the route is on the FPC allowlist (`/`, `/products`, `/products/{slug}`, `/categories`, `/categories/{slug}`, `/search`, `/pages/{slug}`). Off-allowlist routes are never stored; if the leak is on `/cart`, `/checkout`, or `/account`, it is not FPC.
- If `SHOPANDA_DEV_MODE=true` or `cache.full_page.expose_header=true`, inspect `X-Shopanda-Cache: HIT` on the leaked response.
- Dump the cached body (or a HIT response) and search for `csrf_token` values, customer IDs, and display names. Cached shells must be generic: guest vs authenticated chrome only, no per-customer data. Personal bits belong in fragments (PR-1045).
- Confirm vary is only store/language/currency/`guest|authenticated` — a key that includes a session or customer ID is a bug (and also destroys hit rate).

**Fix:** disable immediately with `SHOPANDA_CACHE_FULL_PAGE_ENABLED=false` (or `cache.full_page.enabled: false`) and restart `serve` (FPC is off by default). Flush stored pages: `POST /api/v1/admin/cache/clear` `{"prefix":"fpc:"}` or `{"all": true}` (the latter also clears this process's L1). Do not re-enable until the leaked field is moved to a fragment (`{{fragment "…"}}` / `/fragments/…`) or the route is removed from the allowlist. See `docs/guides/DEVELOPER.md` "Full-page cache" and "Fragments (ESI-equivalent)".

### Signed-in logout returns 403 with full-page cache on

Cached shells ship an empty `csrf_token` until htmx loads `GET /fragments/csrf` (which also mints `shopanda_csrf` when missing). A 403 on logout usually means the fragment never ran or the cookie was blocked.

**Check:**
- Browser network: `/fragments/csrf` is 200 and sets/refreshes `shopanda_csrf`; the logout form’s hidden input is non-empty before submit.
- Theme includes `hx-get="/fragments/csrf"` **inside** the logout form — a path mention elsewhere on the page is not enough; FPC refuses logout forms without that in-form hole.
- JS/htmx is available for authenticated FPC chrome; without it, logout stays empty and POSTs 403. Default theme vendors `themes/default/static/js/htmx.min.js` (1.9.12).

### Cache appears stale after a product/price/content change

**Symptom:** storefront PDP/PLP/CMS/home still shows old price, name, or copy after an admin save, even though the DB is correct. Full-page cache (PR-1044) is serving a HIT; tag invalidation (PR-1046) should have purged it.

**Check:**
- Confirm FPC is enabled (`cache.full_page.enabled` / `SHOPANDA_CACHE_FULL_PAGE_ENABLED`). If off, the stale page is not FPC.
- Confirm the change path publishes an event the FPC subscriber listens to: product create/update, price upsert, stock update, category create/update/delete, CMS page create/update/delete. Content-block-only edits have **no** domain event today — tag purge will not fire for those.
- Logs: look for `cache.fpc_invalidation.done` (or `.error`) with the expected `product:` / `category:` / `page:` / `fpc:listing` / `fpc:navigation` tag around the save time. Missing log ⇒ event not published or subscriber not registered. Product/category create also purges the shared listing/navigation shells so new entities are not stuck behind an old PLP/nav HIT until TTL.
- Immediate remediation while investigating:
  - Prefer tag clear for “everything that rendered this entity”: `POST /api/v1/admin/cache/clear` `{"tag":"product:<id>"}` / `category:` / `page:` / `fpc:listing` / `fpc:navigation` (`cache.write`).
  - `purge-url` deletes the **exact** path (+ filtered query) × store × language × guest/auth keys (store defaults, `?lang=` if present, and `en`). Bare `/products` does **not** clear `?page=2`, `?sort=…`, or other listing variants — pass the full query (including `attr_*` codes that were on the allowlist when cached), or use tag/prefix clear. Accept-Language-only variants still need an explicit `?lang=` on the purge path. API: `POST /api/v1/admin/cache/purge-url` `{"path":"/products/the-slug"}` (`cache.purge_url`); CLI: `app cache:purge-url /products/the-slug`.
  - Nuclear: `{"prefix":"fpc:"}` or `{"all": true}`.

**Fix:** restore event publishing / tag wiring on the write path if a tag was never written or an event never fired. Purge-by-URL is a path-scoped escape hatch, not a whole-PLP broom; do not leave a known miss permanently on TTL alone.

In `SHOPANDA_DEV_MODE`, theme watch reloads templates and then deletes `fpc:` keys plus the in-process category-nav cache. If HTML still looks stale after a template edit, confirm watch is running in that process and that you are not hitting a second replica without watch.

### Headless CORS looks wide open

**Symptom:** a browser on another origin can call `/api/v1` with shopper cookies, or every `OPTIONS` returns 204.

**Check:**
- `http.cors.allowed_origins` / `SHOPANDA_HTTP_CORS_ALLOWED_ORIGINS`. Empty means no CORS headers.
- `*` is anonymous only (`Access-Control-Allow-Origin: *`, no credentials). Combining `*` with `allow_credentials: true` disables the wildcard.
- Credentials require an explicit origin list. `/api/v1` is **not** CSRF-checked — do not enable cookie credentials unless you add CSRF or stay on Bearer.
- CORS is storefront prefixes only. Admin/setup/webhooks/docs must not show `Access-Control-Allow-Origin`.
- Unmatched preflights must fall through (not 204).

See [HEADLESS.md](docs/guides/HEADLESS.md).

### Full-page cache hit rate unexpectedly low

**Symptom:** `shopanda_fpc_requests_total` hit rate (`hit / (hit+miss)`) looks poor for allowlisted routes, or **Operations → Cache → Full-page cache** hit rates look poor after traffic.

**Check:**
- Use the hit/(hit+miss) definition — do **not** treat rising `bypass` (logged-in SkipStore, unsafe HTML, attr-allowlist errors) as an FPC regression.
- Vary-key cardinality: keys include store × language × currency × auth. Unexpected `?lang=` / `Accept-Language` values or many stores multiply keys and lower per-key hit rate.
- TTL too short (`cache.full_page.ttl` / per-route `route_ttl`) — pages expire before reuse (soft expiry becomes miss after delete).
- Allowlist coverage: only templates in the FPC allowlist are cached; everything else never appears in these counters.
- Rising `shopanda_fpc_backend_get_errors_total` (or process-local `backend_get_errors` / Get-failure logs): cache backend issues — fix Redis/Postgres, not TTL. Request outcomes stay hit/miss/bypass when a page was still served.

**Fix:** tighten language/store vary inputs, raise TTL if product freshness allows, add missing templates to the allowlist deliberately, and fix bypass causes (logs: `storefront.fpc.*`).

### A cacheable-looking page never gets cached

**Symptom:** a public GET storefront page always shows `X-Shopanda-Cache: BYPASS` (when exposed) or never appears as an FPC key under `fpc:v1:`.

**Check:**
- Is the route on the allowlist (`/`, `/products`, `/products/{slug}`, `/categories`, `/categories/{slug}`, `/search`, `/pages/{slug}`)? Fail-closed: forgotten == deliberately excluded from the outside.
- Deny prefixes (`/cart`, `/checkout`, `/account`, `/fragments`, …) win even if someone adds the template.
- FPC enabled? `cache.full_page.enabled`.
- Response actually storable (200 HTML, under size cap, no CSRF/PII holes, CSP nonce storable)?

**Fix:** add the template only if it is safe to share; otherwise keep it uncached. Do not lower safety checks to force a HIT.

### Stale content despite tag invalidation

**Symptom:** same as “Cache appears stale…” but logs show `cache.fpc_invalidation.done` and/or `shopanda_fpc_purge_total{trigger="tag_invalidation"}` is rising on **serve and/or worker** scrapes (keys deleted — not soft-TTL).

**Check:**
- Did the **entity's tag** attach when the page was stored? A render that omits `product:` / `category:` / `page:` / `fpc:listing` / `fpc:navigation` leaves a page that purge-by-tag cannot find — confirm tags on write (storefront `AddPageTags`).
- Search lag: event-time purge can refill from a still-stale index before reindex finishes; post-index purge (`AfterProductsIndexed`) should clear again — check reindex job completion.
- Wrong vary key still serving (other language/auth) — purge-url language expand or tag clear.
- Serve admin “Keys purged (tag)” can stay near zero while the worker scrape shows healthy tag deletes — always check both processes.

**Fix:** fix tag attachment on the render path; wait for / trigger reindex; use tag clear or purge-url for the exact vary.

### Why can't I add this product to cart

**Symptom:** storefront or API add-to-cart / qty increase returns **422**. Greppable messages operators see (qty **decrease** / **remove** skip purchase checks, but pricing or tax errors in the remaining cart can still block them):

| Message | Usual cause |
| --- | --- |
| `product is not available for purchase` | Status not `active` (always-on) |
| `grouped products are not purchasable` | Grouped **parent** (always-on; sell member SKUs) |
| `product requires a linked parent and is not purchasable` | Empty assignment context, or required **bundle leaf** with the flag (always-on) |
| `downloadable product has no attached file and is not purchasable` | No attached file, or required bundle leaf missing a file (always-on) |
| `product is not currently purchasable` | Purchasable axis false under enforce: AutoBasis (`no_stock` / `no_price` / `status_inactive`), forced-`hidden` Purchasable, or missing visibility snapshot — **only when enforce is on** (facts unwired when enforce is off) |
| `cart item already has a different linked parent assignment` | Second add, qty increase, or guest-claim merge (`mergeIntoCustomerCart`) onto a line that already has a different stamp |
| `only ` / ` available` (N varies; e.g. only 3 available) | Qty above available stock |
| `price is not available` / `bundle cannot be reserved` | Missing unit price / dynamic-bundle reserve fail |
| `linked parent is not eligible for this product` / `linked parent instance is not owned by this customer` / `sign in to assign this product to an owned instance` / `linked parent product does not match the selected instance` / `cannot assign a unit that is on an open or refunded return` | Named-parent `CheckPurchase` (assignment) |
| `tax calculator: no "digital" rate` / `is 0 bps` / `refusing 0% VAT` | Digital-tax fail-closed (PR-1076) during cart recalculate |

**Check:**
1. Open the product in admin (`/admin/products/{id}`). Confirm **Status** is `active` — draft/archived fail even when visibility enforce is off (PR-1054). Always-on (enforce irrelevant): Status, grouped parent, linked-parent empty context, missing downloadable file — match the 422 strings above.
2. **Purchasable AutoBasis** (`no_stock` / `no_price` / `status_inactive`) is live **only with** `catalog.visibility.enforce: true` (or `SHOPANDA_CATALOG_VISIBILITY_ENFORCE=true`). Do **not** list `requires_linked_parent` / `no_downloadable_file` as AutoBasis inputs — those are separate always-on gates (admin badge may still show them). Modes are always editable; see ["Catalog visibility enforcement"](#catalog-visibility-enforcement-pr-1057).
3. Linked-parent 422 → ["Why isn't my ticket sellable"](#why-isnt-my-ticket-sellable) (assignment stamp / deferred vs named parent). Conflicting stamp on a second add, qty increase, or guest-claim merge → `cart item already has a different linked parent assignment` (table above).
4. Downloadable with no file: **Downloadable files** tab must show ≥1 attachment (PR-1074 language) — not the grants section (grants are for “paid, no grant”).
5. Grouped parents are never purchasable by design; sell the member SKUs (**Grouped** tab).
6. Bundle: required leaves inherit the linked-parent / missing-file always-on rejects. Digital SKUs: a missing/zero digital VAT rate fails closed at add (PR-1076) even when stock/price look fine.

**Fix:** restore stock + price + active when enforce is on and AutoBasis is the miss; for SEO OOS keep Catalog/Search/Individually force-`visible` and leave Purchasable `auto`/`hidden` — do **not** force Purchasable `visible` as the cart fix. Clear `requires_linked_parent` or stamp a valid assignment context; attach a downloadable file before expecting purchasability. Detail: ["Catalog visibility enforcement"](#catalog-visibility-enforcement-pr-1057).

### Why is this product not visible

**Symptom:** product missing from PLP/category/search/suggest/carousel/sitemap, or PDP / variants / reviews return **404**, while admin still shows the product.

**Check (four-axis default-rule checklist — PR-1055):**
1. **Admin vs storefront:** admin always sees every product. Storefront **axis** gates (Catalog / Search / Individually / Purchasable) apply only when `catalog.visibility.enforce` is on. With enforce **off**, search applies **no** visibility-axis filter; sitemap still **status**-filters to active; Individually (PDP 404 for hidden modes) only after enforce. A "hidden" mode is preview-only (admin caption **"Enforcement off — preview"**). Status = active (PR-1054) still applies to public reads either way.
2. On the **Visibility** tab, walk each axis that should apply to the missing surface:
   - PLP / category / carousels → **Catalog**
   - Search / suggest → **Search**
   - Direct PDP → **Individually**
   - Cart/checkout → **Purchasable** (separate incident above)
3. For each axis in mode `auto`, AutoBasis needs **active** + stock qty > 0 + a price (category assignment is off by default). **Grouped parents** never resolve `HasPrice` / `QuantityPositive` under AutoBasis — force Catalog/Search/Individually `visible` or they vanish from PLP under AutoBasis. Virtual/downloadable use their **own** stock row; force-visible on a display axis ≠ sellable at zero stock. Failure codes under the axis name the miss. Mode `hidden` hides that axis **once enforce is on**; with enforce **off** it is badge/preview-only (same as Check 1). Mode `visible` on a draft/archived product still shows **"Forced visible — not live (product inactive)"** — public reads stay active-only.
4. **Canonical cutover:** migrate → full `search:reindex` → **then** enable enforce. Never enable first. See ["Search reindex"](#search-reindex-searchreindex) (PR-1079). After enforce is correctly on, on-save reindex (PR-1080) keeps stock/price/file edits fresh — a full reindex is not required for every later edit.
5. Stale PLP after restock/price: FPC may still be serving a HIT — see ["Cache appears stale after a product/price/content change"](#cache-appears-stale-after-a-productpricecontent-change).

**Fix:** flip Status to `active`; fix stock/price (or force Catalog/Search/Individually `visible` for SEO-critical OOS / grouped parents while leaving Purchasable `auto`/`hidden`); follow migrate → full reindex → enable enforce; then rely on on-save reindex for ongoing edits. Full operator notes: ["Catalog visibility enforcement"](#catalog-visibility-enforcement-pr-1057).

### Why isn't my ticket sellable

**Symptom:** a ticket (or other child SKU) with **Requires linked parent** cannot be added to cart / fails checkout with `product requires a linked parent and is not purchasable`, even when stock, price, and Status look fine. Admin Purchasable badge may show `requires_linked_parent` — **expected** when the flag is on and admin resolve uses empty assignment context; that is not the fault by itself.

**Check:**
1. Product edit form: **Requires linked parent** checkbox is on (core field). Flag + empty assignment context → always-on 422 (enforce irrelevant).
2. **Default theme** (`themes/default/templates/product.html`) and the default PDP add-to-cart form do **not** stamp assignment context. Stock-theme add will **422 for every flagged SKU**. Selling requires a theme or API client that posts `assignment_parent_product_id`, `assignment_parent_unit_id`, and/or `assignment_deferred`.
3. Split the stamp kinds:
   - **Deferred** (`assignment_deferred`): eligibility/`CheckPurchase` is skipped at cart time; bind later via **Sales → Assignments**.
   - **Named parent** (`assignment_parent_product_id` and/or `assignment_parent_unit_id`): must pass `CheckPurchase` (sign-in, ownership, eligibility, no open RMA as applicable).
   - If both deferred and a parent id/unit are set, `CheckPurchase` still runs because the parent id/unit is present.
4. **Linked eligibility** tab: rules matter for named-parent purchase and post-purchase assign; they do **not** alone make the SKU purchasable without a stamp.
5. Second add, qty increase, or guest-claim merge onto a line that already has a **different** stamp → 422 `cart item already has a different linked parent assignment` (see cart Greppable table).

**Fix:** turn the flag off if the SKU should sell standalone; or ship a theme/API client that stamps `assignment_parent_product_id` / `assignment_parent_unit_id` / `assignment_deferred` at cart time; complete eligibility rules and Assignments for card↔ticket ops. Specific RequiresLinkedParent case of ["Why can't I add this product to cart"](#why-cant-i-add-this-product-to-cart).

## Planning

| Phase | Status | Doc |
| --- | --- | --- |
| Phase 5 — Mature commerce | **Complete** | [Roadmap](docs/phase-5-maturity/ROADMAP.md) |
| Phase 6 — Merchant-complete admin | Active | [Roadmap](docs/phase-6-merchant-complete/ROADMAP.md) |

## Plugin extension (operators & integrators)

Shopanda plugins are **compile-time registered** — there is no `.so` drop-in loader.

- Enable **core plugins** via config driver switches (`search.engine`, `queue.driver`, …).
- Enable **external plugins** via config flags after they are registered in `cmd/api/register_plugins.go`.
- Failed plugin init is logged as `plugin.init.failed`; the process continues with other plugins.

**Why no `.so` loading?** See [Dynamic plugin loading research](docs/phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md) (PR-544). Adding a custom external plugin requires rebuilding the binary with your plugin import.

**Check plugin status:** startup logs emit `plugin.status` per registered plugin (`active` / `failed`). Use `app help` to list plugin CLI commands when enabled.

## Rate limiting and login lockout

- HTTP rate limiting is **on by default** (`SHOPANDA_RATE_LIMIT_ENABLED=true`). Default budget is **10 requests/second** with a **burst of 20** (both drivers). Rejected requests return `429` / `rate_limited`.
  - `rate_limit.driver=memory` (default): in-process token bucket. Limits are **not** shared across instances — use a gateway/WAF, or switch to redis, for a global HTTP ceiling.
  - `rate_limit.driver=redis`: shared sliding window of burst/rate seconds (microsecond scores) with capacity burst (10/20 → 20 / 2s; 15/20 → 20 / ~1.333s). Requires Redis 5+ (single-node `redis://` / `rediss://` only — Cluster/Sentinel are not supported). Uses a **dedicated** Redis client (200ms I/O, no retries, pool `min(10*GOMAXPROCS, 64)` or `rate_limit.redis.pool_size`) from `rate_limit.redis.url` if set, else `cache.redis.url` — not the cache pool. Keys live under `<prefix>-rl:` where prefix is `rate_limit.redis.key_prefix`, else `cache.redis.key_prefix`, else `shopanda`. When `cache.driver=redis`, `cache.redis.key_prefix` must be non-empty. Uses Redis `TIME` and a per-process random member ID. Circuit: ≥20 requests with ≥50% errors in a 2s window (20×100ms buckets), **or** ≥5 errors within those 2s with no successes (so an admin replica or off-peak pod still opens the circuit). Either trip starts a 5s hold; when it expires exactly one request probes Redis, and a **failed probe starts a new 5s hold**. Traffic slower than five errors per 2s (e.g. one request every 3s) never fills the quiet trigger — each request pays the 200ms timeout. A short Redis stall (e.g. 300ms `BGSAVE`) at high RPS does not trip as long as errors stay below 50% of the window (successes in the same window also block the quiet 5-error rule). At about 3–9 req/s a 300ms stall with nothing succeeding in the same 2s can fill five all-error samples and trip; that is the trade-off for covering quiet instances. Client disconnects (`context.Canceled` / HTTP/2 `RST_STREAM`, and HTTP/1.1 half-close / `nc -N`) write status 499 (no 429 body, no `ratelimit.rejected` log, no token) so HTTP metrics record 4xx rather than 2xx — they do not admit unmetered work or trip the circuit (a Redis timeout that returns after the client is already gone is still Canceled — no backend-error metric, no circuit sample). A successful half-open probe still closes the circuit if that client has gone, so live traffic does not stay on the outage fallback. Redis I/O runs on `WithoutCancel` so a disconnect cannot abort the script. The **memory** driver ignores the request context, so the same half-close is admitted and counted there. `on_error`: `open` (default, admit), `closed` (deny), `local` (in-process token bucket — recommended for auth; per-instance, so N replicas admit N× the configured budget during an outage). If Redis is down at startup and **global** `on_error` is not `closed`, serve starts with the circuit open (`ratelimit.redis.unavailable`, including `fail_closed_routes` when any `per_route` is `closed`) instead of crash-looping. Per-route `closed` still 429s for the 5s hold. This limiter fail-open does **not** apply when `cache.driver=redis`: the cache plugin still PINGs via `ConnectURL` and refuses to start. Backend errors increment `shopanda_ratelimit_backend_errors_total` (`limiter` = `default` or `route:<path_prefix>`, `reason=error|circuit_open|pool_timeout`) — **sum over `limiter`** for a factory-wide outage. Redis errors are sampled in logs (≤1/10s). Fail-closed outage 429s omit `client_ip`; local-fallback denials keep it.
  - **Redis connections:** each `serve` instance opens a limiter pool of `min(10*GOMAXPROCS, 64)` (override with `rate_limit.redis.pool_size`) **plus**, when `cache.driver=redis`, the cache client's own pool (`10*GOMAXPROCS`, not capped here). Example: 8 cores with a CPU limit → limiter 64 + cache 80 = 144/instance; 15 instances ≈ 2,160, under Redis's default `maxclients` of 10,000. A 64-core host **without** a CPU limit: limiter 64 + cache 640 = 704/instance; 15 instances ≈ 10,560 — raise `maxclients` or set both pools explicitly. Go 1.26+ respects cgroup CPU limits for `GOMAXPROCS`.
  - **Switching from `memory` to `redis` with unchanged rate/burst values does not preserve burst-recovery timing**, only the peak burst size and steady-state average rate. A token bucket (`memory`) refills continuously — e.g. the default rate=10/burst=20 admits one more request ~100ms after exhausting the burst. The sliding-window counter (`redis`) instead requires the *oldest* of the burst's entries to age out of the window (here, 2 seconds) before admitting a new one — for a true simultaneous burst, that's a ~2-second wait, not ~100ms. If a deployment's config was tuned around the `memory` driver's fast recovery, switching to `redis` unchanged can produce unexpected `429`s on traffic the `memory` driver would have allowed.
- **Auth vs general API:** `defaults()` has **no** auth `per_route`. The example used to cap all of `/api/v1/auth` at 5/s burst 10. That is now split: `/api/v1/auth/login` (burst 2), `/login/mfa` (burst 5, own prefix), `/register`, and `/password-reset` with `on_error: local` so a Redis outage does not refuse logins. Two rapid login attempts then a 429 is expected. `/auth/me` and `/logout` use the default 10/s burst 20 (looser than the old auth-wide cap). Copying the example into an existing deploy is a real policy change — tune before regenerating.
- **Probes and automated traffic:** `/healthz` is unmetered (static 200, no DB). `/readyz` sits outside `RateLimitMiddleware` and always uses an **in-process** per-IP limiter (this instance's DB pool, never Redis). Payment webhook callbacks (`POST /api/v1/payments/webhook/*`) stay on the **default** limiter — provider source IPs are shared, so a dedicated tight `per_route` false-positives on legitimate delivery bursts. Put a webhook ceiling on the gateway if you need one.
- Failed password logins are throttled by **IP + normalized email** (`auth.lockout`). After `max_failures` within the lockout window, login returns `429` (`too many login attempts, try again later`). Wrong-password responses remain uniform `unauthorized` before the threshold.
- **Reserve-before-verify:** each login atomically increments the counter before password comparison, so a concurrent batch for the same IP+email cannot evaluate more than `max_failures` guesses. A successful login clears the reservation (and prior failures) via compare-and-subtract.
- **Sliding window:** each reserved/failed attempt refreshes the full `auth.lockout.window` TTL. Continued failures can extend lockout; quiet time of one full window after the last failure is when lockout ends (or a successful login clears the counter).
- **Multi-instance lockout:** `auth.lockout.store=cache` (default) shares counters via cache (postgres/redis) using atomic `Incr` (sliding window TTL). `store=memory` is single-instance only; startup logs a warning if selected.
- Behind a reverse proxy, set `rate_limit.trusted_proxies` in YAML (CIDR / IP list; see `configs/config.example.yaml`) so ClientIP for rate limit **and login lockout** is not the proxy address. **Migration:** list every hop that writes `X-Forwarded-For`, including CDN/edge ranges — if only the load balancer is listed, the rightmost untrusted hop is the CDN edge and every shopper behind that POP shares one bucket (and one lockout key). All `X-Forwarded-For` header lines are joined, then walked from the right; `ip:port` and `[v6]:port` hops are accepted (Azure Application Gateway and some other proxies append a port). The first invalid entry falls back to the peer. No env mapping for trusted proxies. The same list gates HSTS (`Strict-Transport-Security`) via `X-Forwarded-Proto: https`.
- If `cache.driver=redis`, `cache.redis.key_prefix` must be non-empty (startup rejects empty/whitespace). An empty prefix would make `SCAN`/`FlushAll` match the whole Redis DB (queues, limiter keys, other apps).

## Secure-by-default config (startup)

- Startup **rejects** DB passwords `changeme` / `shopanda` unless `SHOPANDA_DEV_MODE` is truthy (`1`/`true`/`yes`).
- `sslmode=disable` / `prefer` / `allow` (or missing `sslmode` on `DATABASE_URL`) are allowed only with truthy `SHOPANDA_DEV_MODE` **and** a local DB host (`localhost`, `127.0.0.1`, `::1`, or compose service `postgres`). Otherwise use `require` / `verify-ca` / `verify-full`. When `DATABASE_URL` is set, only that DSN is checked (YAML/`SHOPANDA_DATABASE_*` are not merged).
- Plaintext password-reset token logging requires **both** `SHOPANDA_DEV_MODE` and `SHOPANDA_DEV_LOG_RESET_TOKENS` truthy. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md).
- Local `docker compose` defaults `SHOPANDA_DEV_MODE=true` and `SHOPANDA_DATABASE_SSLMODE=disable` (stock Postgres image has no TLS) via `${…:-…}` — **omit/empty still applies those defaults**. For production-like compose: set `SHOPANDA_DEV_MODE=false` (or `0`/`no`), override `SHOPANDA_DATABASE_PASSWORD` with a strong non-default value (**do not keep compose `changeme`**), and set `SHOPANDA_DATABASE_SSLMODE=require|verify-ca|verify-full`. `.env.example` leaves sslmode unset so the compose default applies after `cp`.

## Database connection pooling (PgBouncer)

- If PgBouncer (or similar) sits in front of PostgreSQL in **transaction-pooling mode**, set `database.query_exec_mode: exec` (or `SHOPANDA_DATABASE_QUERY_EXEC_MODE=exec`). Otherwise pgx defaults to server-side prepared statement caching, which does not survive that pooling mode — symptom: `prepared statement "..." already exists` / `does not exist` errors under load.
- Leave unset for a direct connection to PostgreSQL — it trades away a real performance optimization and should only be set when a transaction-pooling proxy is actually in front of the database. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md) ("Consider connection pooling").

## Catalog visibility enforcement (PR-1057)

Storefront axis gates (`VisibleInCatalog` / `VisibleInSearch` / `VisibleIndividually`) are **off by default**.

**Enable only after a deliberate choice:**

```yaml
catalog:
  visibility:
    enforce: true
```

Or `SHOPANDA_CATALOG_VISIBILITY_ENFORCE=true`, then restart `serve`.

**Why default off:** migration `086` set every product’s modes to `auto`. AutoBasis requires a stock row with `quantity > 0` and at least one price. With enforce on, OOS products, products with no stock row, and products with no price drop from listings, search, suggest, carousels, and the sitemap, and their PDP / variants / reviews return **404**. That is intentional AutoBasis behaviour, not a soft “hide from PLP only” rule.

**Cart and checkout (PR-1058):** the same flag turns on the `Purchasable` axis for cart mutations and checkout. The product `Status` check (PR-1054) applies whether enforce is on or off.

| Path | Behaviour when `Purchasable` resolves false |
| --- | --- |
| Add to cart, quantity increase | 422 `product is not currently purchasable` |
| Quantity decrease, remove line | Allowed, so customers can clear blocked lines |
| Guest cart claim on login | Blocked lines are **dropped** and the rest merge; logged as `cart.guest.claim.lines_dropped` (`guest_cart_id`, `variant_ids`) |
| Checkout (`validate_cart`) | 422 naming the first blocked variant; the order is not created |

- With mode `auto`, a product that goes out of stock or loses its last price becomes non-purchasable, so existing carts holding it fail checkout until the line is removed. That includes carts already mid-checkout.
- A product with no visibility snapshot (e.g. non-active) is denied (fail-closed).
- A visibility-facts DB error returns 500 and aborts the claim or checkout; it never drops lines.
- Admin API contexts bypass the axis but not the `Status` check.
- **`requires_linked_parent` (PR-1068):** a product with this flag true is not purchasable while the resolved assignment context is empty. Cart add stamps a non-empty request context onto the line (`assignment_parent_product_id` / `assignment_parent_unit_id` / `assignment_deferred`); the default theme does not stamp these fields. Qty increase, guest-cart claim, and checkout `validate_cart` read the line first, then the request. A visibility bypass and a nil facts source do not skip it. Required bundle leaves are rejected the same way, including at `BundleService.Add`. The 422 text is `product requires a linked parent and is not purchasable`. Claim drops a line that has no stored assignment and logs `cart.guest.claim.lines_dropped`. A line that already carries a parent or deferred stamp is kept. Display axes (PLP, search, PDP) are unchanged. `Deferred: true` skips existence/eligibility/ownership at cart time (bind later via Assignments). A named parent product and/or unit id must pass `CheckPurchase` (existence, eligibility, ownership, open-RMA rules as applicable — shipped in PR-1070). Storefront must not set `Deferred` on every request. Set the flag with admin `POST`/`PUT` `requires_linked_parent` or product CSV (`requires_linked_parent`, create-only). The product response field is `RequiresLinkedParent`. The admin purchasable badge shows `requires_linked_parent` when the flag is set (empty admin context — expected).
- Frequent `cart.guest.claim.lines_dropped` warnings after enabling usually mean OOS products are still sitting in guest carts. Expected; no action needed unless volume is surprising.
- **Admin assignment API (PR-1071):** `assignments.read` lists a card (`GET /api/v1/admin/assignments`, default `status=assigned`, cap 100, `has_more` when another row exists) and the current child row. Both reads include live order status, payment status, and customer id for the parent and the child, and `forced` when the bind was an override. `assignments.write` assigns and revokes. Admin, Manager, and Support have both; Editor has neither. A 409 is an already-assigned child. A 422 is an invalid unit id (`foo/bar`), a missing line, a missing catalog variant, a child that does not require a linked parent, an ineligible parent, or a cancelled/failed/unpaid order. Support may force-bind an unpaid or cancelled unit: `assignments.write` includes `force`. That is an intentional entitlement override. `force: true` with a non-empty `reason` (at most 500 characters) stores `forced_at` and `forced_reason` only when a side is not bindable, and still leaves the order unpaid or cancelled. A reason longer than 500 characters is 422 only when `force` is true. The stored text is returned as `reason`. `force` on an already-bindable pair is 422 `force not applicable`. List, current, and revoke return the same live order facts. If that fact read fails, the response is still 200 with the assignment rows and empty fact fields, and the API logs `admin.assignment.facts_failed` with `route` (`list`, `current`, or `revoke`) and `error`. List logs that once, with `count` set to the row count. `assignment_id` is included when that read is a single row. A failed fact read does not undo a revoke. Revoke of an already-revoked id is 200 and does not write another audit entry. Contract detail is in `docs/guides/DEVELOPER.md`. **Returns (PR-1072):** approving (or refunding) a return for a ticket/child unit auto-revokes that unit's current `ProductAssignment` (`assigned` or never-assigned `unassigned`) in the same transaction as the status write — wrong-status calls do not revoke. Returning a card/parent while tickets are still assigned is **blocked**, unless those tickets are included on the **same** RMA (same-order card+tickets). `ReturnableLines` hides a card that would 422. Assign (customer or admin), checkout `CheckPurchase`/`RecordPurchase` stamping onto a parent unit, and `ListOwnedParents` listing are blocked or filtered while a unit is on an open or refunded RMA (`requested`/`approved`/`received`/`refunded`; rejected/cancelled allow re-bind). `ReturnableLines` keeps a card only when its assigned same-order tickets are also mutually returnable (iterative filter); selecting the card requires submitting those ticket lines together. No silent cascade. Eligibility CSV: `import`/`export:linked-eligible-parents` and `import`/`export:linked-eligible-categories`. Admin GUI: product section **Linked eligibility** and **Sales → Assignments** (unit lookup + assign/revoke). Assignment CSV is not supported.

**Product CSV visibility + status (PR-1081):** export/import columns `visibility_catalog`, `visibility_search`, `visibility_individually`, `visibility_purchasable` (`auto|visible|hidden`, case-sensitive — `HIDDEN` is rejected) and `status` (`draft|active|archived`, case-sensitive). Create-only: a non-empty consensus that differs from the stored product skips the whole slug group (including any new SKUs in that file). New exports always include the four visibility tokens and `status` (empty stored → `auto` / `draft`). Old product CSVs without those columns still import — omitted columns do not trip the create-only check. Positional column layout changed (status + four visibility headers sit before `variant_name`); prefer header names, not column indexes. Attribute keys that collide with those exact reserved headers are omitted on export — rename the attribute to round-trip its values. **Downloadable + `active` on CSV create is rejected** (`DownloadableRequiresFileMessage`, same as admin) — import as `draft`, then `import:downloadable-files`, then activate via admin. Successful product creates publish `catalog.EventProductCreated` (Publish-then-PublishAsync). CLI `import:products` registers `rewrite.Subscriber` on that short-lived bus so `/<slug>` rewrites are written (serve alone cannot see the CLI bus), and also batch-Triggers a scoped reindex of created IDs when `ReindexService` is available (logged `import.products.reindex_service_unavailable` otherwise — fall back to `search:reindex`). Publishing the event alone is not enough — the CLI process must register the rewrite listener. **Still non-CSV:** assignment units (`/admin/sales/assignments`), `downloadable_grant_settings` (admin product form / HTTP), downloadable blobs, product↔category links. Prices/stock/attributes/categories CSV pipelines were not re-proven by this PR’s consistency suite.

**Admin GUI (PR-1059):** the product edit form in `/admin` now exposes all four `VisibilityModes` (catalog/search/individually/purchasable) as per-axis selects, independent of whether `catalog.visibility.enforce` is on — this is always available so merchants can set up modes ahead of enabling enforcement. A single axis can be changed at a time: `visibility_modes` in the save request is a **partial** merge, so `{"catalog": "hidden"}` alone does not touch the other three axes.

Each axis shows a storefront-facing badge — **Auto — visible/hidden**, **Forced visible**, **Forced visible — not live (product inactive)**, or **Forced hidden**. There is no per-badge "Would be …" prefix (PR-1059 CR — simplified copy); instead, when `catalog.visibility.enforce` is off, one section-level caption reads **"Enforcement off — preview"**, since the gate described isn't actually live yet. A second section-level caption, **"Preview as of form status (save to persist modes)."**, is always shown: the main form's `status` select has its own `change` listener that re-renders every axis badge **and failure list** from the **currently selected** status, so an operator can preview the effect of a pending (unsaved) status change before saving — this recompute (PR-1059 CR round 3) is driven entirely by the GET payload's `input_failure_codes` (the same `AutoBasisFailureCodes` list with `status_inactive` always excluded), combined client-side with whichever status is currently selected, so switching the status select away from the saved value never leaves a stale badge or a stale/missing `status_inactive` failure-list entry behind. The **"not live (product inactive)"** case matters operationally: forcing an axis `visible` on a draft/archived product does **not** put it on the storefront — `LoadMany` is active-only regardless of the forced mode — so the badge always reflects that effective (public-gate) truth, not the raw override.

Auto axes with a failing `AutoBasis` list the specific stable failure code(s) under that axis (`status_inactive`, `no_stock`, `no_price`, `no_store_assignment` (reserved — unreachable today), `no_category`), shown as merchant-facing text. This is read against `LoadManyAnyStatus`, so it explains draft/archived products too, not just active ones — useful for pre-launch QA before flipping a product to `active`. If the visibility-facts read itself fails — including a missing snapshot, e.g. a product deleted concurrently with the GET (PR-1059 CR round 3 — this used to silently omit the whole `visibility`/`visibility_error` pair) — the product page still loads (200) with an explicit **"Visibility facts unavailable — reload to retry"** banner in place of every badge and a `visibility_error` noted in the API response — this is a display-degradation, not a product-read failure, and is unrelated to the fail-closed/500 behavior described above for the cart/checkout enforcement path. Do not confuse this with the unrelated "pending" state (no payload, no error) shown as `"<Mode> — computed after save"` for a not-yet-saved create — operators should expect a reload-to-retry prompt whenever `visibility_error` is present, and the pending state only for a genuine not-yet-saved create.

**Before enabling in production:**

1. Decide whether OOS/unpriced PDPs should 404, or force display axes to `visible` (leave `purchasable` as `auto`/`hidden`) for SEO-critical SKUs.
2. Spot-check a few OOS and unpriced products under enforce in staging.
3. After enable, stock and price events purge `fpc:listing` as well as `product:{id}` so restocked/priced products can reappear on cached PLPs. FPC keys include the enforce flag, so pages cached while enforce was off are not reused after the flip (no manual FPC flush required for correctness).

**Deep / sparse REST listings:** `GET /api/v1/products` and category product lists return `meta.has_more`, `meta.truncated`, and (when more may exist) `meta.next_after`.

- **Always resume with `?after=<next_after>&limit=N`**. When `after` is set, `offset` is ignored. Do **not** advance `offset` by `len(products)`.
- `truncated: true` means the scan budget stopped early (short or empty page, or mid-skip); continue with `after` — the cursor carries any remaining skip.
- Empty `products` with `has_more: false` is end of catalog. `has_more: true` can still yield one trailing empty page when only hidden rows remain.
- Cursor resume depth is capped at 100000 repository rows. At that limit `meta.depth_limited` is true, `has_more` is false, and `next_after` is omitted — the server never issues a cursor it would reject. Classic `offset` is not capped the same way and can still go deeper until keyset pagination lands. Tampered / over-cap `after` values return 422.

Stock/price events purge `fpc:listing` only while enforce is on — every order can invalidate PLP caches. Prefer enabling enforce during a low-traffic window, or accept lower FPC hit rates until threshold-crossing invalidation lands.

Long-term: keyset `(created_at, id)` or SQL-level visibility predicates (post-1057). Offset-based cursors can drift when products are inserted or deleted between requests.

## Admin product type

- Product `type` is **mutable** via admin create/update (`simple` \| `virtual` \| `bundle` \| `grouped` \| `configurable` \| `downloadable`). Create may omit `type` (defaults to `simple`); update must omit the field to leave type unchanged — an explicit empty/whitespace `type` is rejected.
- Changing a live SKU between physical and `virtual`/`downloadable` changes whether **new** checkouts require a shipping method (derived in cart validation). Carts already mid-checkout keep the shipping requirement cached from when they last validated.
- `bundle` / `grouped` composition is editable in admin and CSV. `downloadable` file attach/settings are admin HTTP + CSV + product form (PR-1078); grants issue on `order.paid` (PR-1075); customers download via `POST /api/v1/account/downloads/{grantID}` (PR-1077); support revoke/re-issue uses `downloads.write` on order/customer detail. `configurable` still has incomplete storefront/admin behavior until its track lands.
- **Replace file (PR-1078 blast radius):** `PUT …/downloadable-files` retargets grants by **asset id**, not "this product only". If other products still attach `old_asset_id`, the API returns **422** unless the body sets `global_retarget=true` (then every grant on that blob moves — including other products' order lines). Response includes `grants_moved` / `grants_revoked`. Expired old grants deleted to free UNIQUE `(order_item_id, asset_id)` vanish (IDs gone; no revoke webhook for those rows). Live UNIQUE conflict publishes `download.grant.revoked`. Subscribe to `download.grant.issued` / `revoked` / `reissued` on outbound webhooks.
- Bundle mode fields (`pricing_mode`, `stock_mode`, `explode_order_lines`) save with the product when type is bundle. The explode checkbox is enabled only when both modes are dynamic. Component and member links use dedicated routes, not the product PUT: `GET`/`POST /api/v1/admin/products/{id}/bundle-components`, `DELETE .../bundle-components/{componentId}`, and the grouped-members pair. A parent of the wrong type is 422. Removing a link that is not there is 404. Adding a required bundle component past `catalog.MaxBundleRequiredComponents` (50) is 422 and the link is not saved. `products.read` lists; `products.write` adds and removes.
- CLI (operator scope, additive — a missing CSV row does not delete a link): `import:bundle-components` / `export:bundle-components` (`parent_slug,component_slug,quantity,required`; empty `required` means true) and `import:grouped-members` / `export:grouped-members` (`parent_slug,member_slug`). Row failures are `line N:` and do not abort the rest of the file. Product CSV exports stored modes and refuses a non-empty mode that does not match an existing product. It does not update modes; use the admin product PUT for that (it still locks the row and rejects dynamic pricing or stock while an own price or stock row exists).

## Downloadable grants (PR-1075)

**Symptom:** customer paid for a downloadable product but has no grant row.

**Check:**

1. Confirm the product `type` is `downloadable` (or a non-exploded bundle with a **required** downloadable leaf) and `downloadable_files` has ≥1 row for that product/leaf.
2. Confirm `order.paid` fired. Publishers: sync payment success, `$0`/`payment_skipped` checkout, `payment.completed` webhooks (after persist), admin/integration status → `paid`. Pending Stripe initiate does **not** fire `order.paid` (wait for capture webhook). Do not expect grants from `checkout.completed` alone.
3. Look up `download_grants` by `order_item_id = '<orderUUID>/<variantID>'` (same mint as assignment unit ids). Bundle leaf grants use the **parent** line's order_item_id.
4. Global defaults: `catalog.downloadable.default_expiry` / `default_download_limit` (env `SHOPANDA_CATALOG_DOWNLOADABLE_*`). Explicit `default_download_limit: 0` is rejected. Product overrides: `downloadable_grant_settings`.
5. Guest orders issue grants with empty `customer_id` — `ListByCustomer` rejects empty id; list by order item until account-link rebind (follow-up).
6. Approve/refund of a return **revokes** grants for those lines (same TX as assignment revoke). Operator consequence: refunded downloadables lose access.
7. If `order.paid` published but the async issuer failed, there is no durable outbox in this PR — re-deliver the payment webhook / admin paid update / `ApplyStatus(paid)` (they republish `order.paid` on already-completed/already-paid) or call `IssueForOrder`. UNIQUE + IssueIdempotent leave existing rows unchanged (including revoked — paid redelivery does **not** resurrect). Support re-issue is admin `POST /api/v1/admin/download-grants/{id}/reissue` (or the order/customer GUI). Delivery endpoint is PR-1077. Confirmation email on republish is claimed in `notification_dedupe` `(subject_key, kind=order_confirmed)` as `pending` before enqueue and marked `completed` after success (queue job-id alone is not enough on SQS/RabbitMQ); enqueue failure deletes the pending claim so a later paid signal can retry.

## Digital-goods tax class (PR-1076)

- `downloadable` lines use tax class `digital`; `virtual` and other types (including bundle parents) use `standard`. Cart + checkout share `EnsureTaxClasses` (existing Meta keys are not overwritten).
- **Release requirement:** run `shopanda migrate` (migration `108` creates `digital` rows from every non-zero `standard` rate, `ON CONFLICT DO NOTHING`). Optionally run seed afterward — TaxSeeder is a second idempotent backfill via `tax.RateRepository` (`ListByClass` + `CreateIfNotExists`). Fail-closed must not ship without migrate-path data.
- **Fail-closed:** if a line is `digital` and there is no usable `tax_rates` row for the destination country (+ store scope fallback), or the digital row has `rate = 0`, checkout recalculate / pricing returns a **validation error** — it does **not** apply 0% VAT. Intentional 0% VAT must use class `zero`, not a zero-bps `digital` row. Operators must still ensure destination coverage for every country they sell into.
- **Zero-rate policy:** migration/seed do **not** copy zero-bps `standard` into `digital`. Application `Upsert` of a `standard` rate does **not** create a matching digital row (no Tax settings UI / RateService yet).
- **After migrate — new destinations:** when you add a new `standard` rate for a country (or store scope) after migration `108`, you must also insert a matching `digital` row. Migrate only backfills rows that exist at migrate time; later standard-only inserts leave downloadable checkout fail-closed for that destination.
- **Existing `digital` rows with `rate = 0`:** after this release they fail checkout. `UPDATE` the bps, `DELETE` the row (then insert a correct digital rate), or move intentional 0% VAT to class `zero` (and stop using class `digital` for that line).
- **Hook / Meta override risk:** a trusted hook that pre-sets `tax_classes` for a downloadable to a non-digital class (e.g. `standard`) skips digital fail-closed for that line — treat as trusted-VAT risk.
- Place of supply at **checkout** is the address country (`tax_country`). Cart tax preview may use the **store** country when no destination is set — not OSS place-of-supply.
- There is **no Tax settings admin UI** yet. Insert a digital rate with SQL, e.g.:

```sql
-- Insert digital for a destination (leave existing digital untouched).
INSERT INTO tax_rates (id, country, class, store_id, rate)
VALUES (gen_random_uuid(), 'FR', 'digital', '', 2000)
ON CONFLICT (country, class, store_id) DO NOTHING;

-- Or copy non-zero bps from that country's standard rate (use after adding a new standard destination):
INSERT INTO tax_rates (id, country, class, store_id, rate)
SELECT gen_random_uuid(), country, 'digital', store_id, rate
FROM tax_rates WHERE class = 'standard' AND country = 'FR' AND rate > 0
ON CONFLICT (country, class, store_id) DO NOTHING;

-- Fix a leftover zero-bps digital row (example: set to standard bps, or DELETE):
-- UPDATE tax_rates SET rate = 2000 WHERE country = 'FR' AND class = 'digital' AND store_id = '' AND rate = 0;
-- DELETE FROM tax_rates WHERE country = 'FR' AND class = 'digital' AND store_id = '' AND rate = 0;
```

- Reverse charge / VAT ID is not implemented. OSS CSV columns are unchanged (PR-534). Plugin tax calculators (e.g. `taxdemo`) ignore `tax_classes` — an operator trap if you expect class-aware rates from a flat plugin. Treat live EU digital VAT as needing tax-domain sign-off; this PR is a seam, not compliance-complete.

## Admin store credit issuance

- `POST /api/v1/admin/customers/{customerId}/store-credit/issue` mints store credit; gated by the dedicated `customers.store_credit.write` permission (`RoleAdmin` only by default), not `customers.write`.
- **Cap:** a single issuance is capped at `store_credit.max_issue_amount` (minor units, default `100000`; `0` disables the cap). Set this to your actual risk tolerance — the default is a conservative placeholder, not a business-approved limit.
- **Idempotency:** send an `Idempotency-Key` header on issuance requests. A retried request with the same key (e.g. after a client timeout with an ambiguous response) is a no-op instead of crediting twice; requests without the header are unprotected, matching pre-existing behavior. Uniqueness is per-customer, enforced by a DB constraint (migration `064_store_credit_idempotency.sql`), not just an in-process check.
- Every issuance (success or failure) is written to the admin audit log (`AuditStoreCreditIssue`), same as payments/returns.

## Admin jobs (retry/cancel)

- `GET /api/v1/admin/jobs` (list, filter by `type`/`status`, paginated) and `GET /api/v1/admin/jobs/{id}` (detail, incl. `payload` and `last_error`) are gated by `jobs.read`; `POST /api/v1/admin/jobs/{id}/retry` and `POST /api/v1/admin/jobs/{id}/cancel` are gated by `jobs.write`. Both are admin-only (`RoleAdmin`), same as `audit.read` — no other role has them by default.
- **Retry** only works on a job currently `failed`: transitions its status from `failed` back to `pending`, and resets `attempts` to `0` and `run_at` to now so the worker picks it up again on its next poll. Any other status (`pending`, `processing`, `done`, `cancelled`) returns **409 conflict** naming the job's actual status — retry is never a silent no-op.
- **Cancel** only works on a job currently `pending`: flips it to the terminal `cancelled` status so it is never dequeued. **There is no in-flight cancellation** — a `processing` job cannot be cancelled; the 409 response says so explicitly (`"job is currently processing and cannot be cancelled — ... wait for it to complete or fail, then retry if needed"`), since this is the most likely point of operator confusion.
- **Postgres-only.** Both the read endpoints and retry/cancel require the configured job queue driver to satisfy the `jobs.Reader`/`jobs.Admin` ports — true for the built-in Postgres queue, not for a broker-backed driver (Redis, RabbitMQ, Kafka, SQS). If a non-Postgres driver is configured, `serve` fails at startup with a clear error rather than silently omitting these routes.
- Every list/get/retry/cancel call is written to the admin audit log (`job.list`, `job.read`, `job.retry`, `job.cancel`; `ResourceType: "job"`), same as payments/returns/store credit.
- No bulk operations yet (e.g. "retry all failed of type X") — single-job actions only.
- **CLI equivalents** (PR-1032, for deploy scripts/CI that shouldn't need HTTP + auth): `app jobs:list [--type=X] [--status=Y] [--limit=N] [--offset=N] [--json]`, `app jobs:show <id> [--json]`, `app jobs:retry <id>`, `app jobs:cancel <id>` — same `jobsApp.Service` calls as the HTTP routes above, same retry/cancel status rules, non-zero exit on any failure. `jobs:retry`/`jobs:cancel` write to the same admin audit log the HTTP routes do (`job.retry`/`job.cancel`), with `AdminID` set to `cli:<os-user>` (there's no authenticated admin session in a CLI invocation) instead of a real admin user ID — check the audit log's `admin_id` column for that prefix when tracing a CLI-initiated action. `jobs:list` defaults to the same limit/offset the HTTP route does and prints a "there may be more" hint when a page comes back full, instead of silently truncating.

## Admin cache (stats / clear)

Cache occupancy/clear also has an admin GUI under **Operations → Cache** (PR-1043). Purge-by-URL is API/CLI only today (`POST /api/v1/admin/cache/purge-url`, `app cache:purge-url`).

- `GET /api/v1/admin/cache/stats` is gated by `cache.read`. `POST /api/v1/admin/cache/clear` targeted clears (`{"prefix":"..."}` / `{"tag":"..."}` / `{"key":"..."}`) are gated by `cache.write`; `{"all": true}` requires the distinct `cache.clear_all` permission — a full flush is a bigger blast radius (every cache consumer pays a cold-cache penalty at once, including the storefront full-page cache) than a targeted clear. `POST /api/v1/admin/cache/purge-url` requires `cache.purge_url` (FPC path escape hatch). All four permissions are admin-only (`RoleAdmin`) by default, same as `jobs.read`. 403 is a well-formed selector the caller is not allowed to run; 422 is an invalid body (missing/empty/whitespace selector, `{"all": false}`, more than one selector, or a non-cacheable purge path).
- **Stats** are approximate occupancy, not a live hit/miss rate for L2. Postgres runs `COUNT(*)` with a 1.5s statement cap, then falls back to `pg_class.reltuples` (planner estimate; can lag `ANALYZE`). When that fallback is used, the payload sets `l2.approximate: true` — treat `keys`/`tag_rows` as a stale estimate, not an exact count. Expired rows the cleanup job has not swept yet are included. Redis reports `DBSIZE` of the configured database (the whole DB, not just this store's key prefix) and `used_memory` from `INFO memory` when present. L1 (this process's permission catalog and, when the storefront is enabled, category-nav tree) reports entry count plus process-lifetime hit/miss counters — no backend round trip. Successful HTTP `cache.stats` calls are **not** audit-logged (the endpoint is polled); failures still are. CLI `cache:stats` is not audited.
- **Clear by prefix/tag/key** calls the same `DeleteByPrefix` / `DeleteByTag` / `Delete` the rest of the app uses. An empty/whitespace selector is rejected so `{"prefix":""}` cannot sneak a full-table wipe past `cache.clear_all`. Prefix is a **literal** string — Redis glob metacharacters (`*`, `?`, `[`, `\`) are escaped, so `{"prefix":"*"}` with only `cache.write` deletes keys that literally start with `*`, not the whole store. Exactly one selector is required. Targeted prefix/key clears do not return a `deleted` count (tag/`all` do).
- **Clear all** scan-and-deletes L2, then clears this process's L1 stores. On Redis it **never** calls `FLUSHDB`/`FLUSHALL` — only keys under `cache.redis.key_prefix` are removed, so rate-limit ZSETs and queues sharing the instance are left alone. Empty prefix is refused at FlushAll (and rejected at config load). Redis SCAN is not a point-in-time snapshot: a concurrent `SetWithTags` during a flush can leave tag-index orphans that `cache.cleanup` (`DeleteExpired`) later reclaims. `deleted` is **value keys** only (Postgres `cache` rows; Redis keys that are not `tag:` / `__keytags:` / `__purge:`). Postgres FlushAll is an exact delete of both tables in one statement — on a large UNLOGGED cache it can stall the admin request and contend with writers. Other API replicas keep their own L1 until TTL (seconds) or their own in-process invalidation events; the CLI process has no L1 at all.
- Every **clear** (including targeted) is written to the admin audit log (`cache.clear`; `ResourceType: "cache"`) with the mode and target.
- **When cached content looks stale:** first try a targeted clear of the entity prefix (`product:<id>:`) or tag; use `--all` / `{"all": true}` only when you need a cold cache everywhere. Then wait out L1 TTL on other replicas (permission catalog 30s, storefront category tree 45s) or restart those processes.
- **CLI equivalents:** `app cache:stats [--json]`, `app cache:clear --prefix=<p> | --tag=<t> | --key=<k> | --all` — same `cache.AdminService` as the HTTP routes. **The CLI has no RBAC** (same pattern as `jobs:retry` / `jobs:cancel`): anyone who can run the binary as the deploy user can `cache:clear --all`. That is an ops trust boundary, not HTTP `cache.clear_all` parity. `cache:clear` writes `cache.clear` to the audit log with `AdminID` `cli:<os-user>`. `cache:stats` from the CLI will show empty L1 (this is a separate process, not `serve`).

## Admin schedules (list/trigger/enable/disable)

- `GET /api/v1/admin/schedules` is gated by `jobs.read`; `POST /api/v1/admin/schedules/{name}/trigger`, `.../enable`, `.../disable` are gated by `jobs.write` — same permissions as job admin (schedules and jobs are the same operational surface).
- **The scheduler runs as a separate OS process from the API server in production** (`./app scheduler`, not embedded in `./app serve` — see "Runtime Modes" in the Developer Guide). Because of that, this admin surface is Postgres-backed, not read from this server's own memory:
  - **List** always reflects reality — whichever process is actually running (the standalone `scheduler` command in production, or `serve` with an embedded scheduler in dev) upserts its registered task names/specs into `scheduler_tasks` every time it starts.
  - **Enable/disable** always works and takes effect in the real running scheduler without it needing a restart — its tick loop checks the same `scheduler_tasks.enabled` column on every tick before firing a task.
  - **Trigger is the one operation that needs a live scheduler in *this* process.** In a standard production deployment (`serve` without an embedded scheduler), triggering returns **409 conflict** — `"this server process has no embedded scheduler to trigger from"` — because there is no in-process function to call. It works immediately in `dev` mode (`./app dev`, which embeds the scheduler by default) or any `serve` deployment that explicitly opts into embedding.
  - If you *do* see `409` with a different message — `"is registered but not part of this process's local scheduler"` — that means more than one process is embedding a scheduler with different task sets (e.g. two `serve --embed-scheduler` instances with different plugin configs). Running more than one embedded scheduler is unsupported: each one independently fires its own tick, so more than one active at a time double-fires every scheduled task. Fix by running exactly one embedded scheduler, or none (standalone `./app scheduler`, the recommended production setup).
- A disabled task can still be triggered manually — disabling only stops the *automatic* tick from firing it, exactly like a paused-but-manually-runnable job.
- Unknown task name: **404**, on any of the four endpoints.
- Every list/trigger/enable/disable call is written to the admin audit log (`schedule.list`, `schedule.trigger`, `schedule.enable`, `schedule.disable`; `ResourceType: "schedule"`).
- **CLI equivalents** (PR-1032): `app schedule:list [--json]`, `app schedule:enable <name>`, `app schedule:disable <name>` call the exact same `schedulerApp.Service` methods as the HTTP routes above (Postgres-backed, always work regardless of process), and write to the same admin audit log the HTTP routes do (`schedule.enable`/`schedule.disable`/`schedule.trigger`), with `AdminID` set to `cli:<os-user>` instead of a real admin user ID (there's no authenticated admin session in a CLI invocation).
- **`app schedule:trigger <name>` does not call the same method the HTTP route does** — the CLI process has no embedded scheduler to reuse either, so instead of hitting the same 409 a `serve`-without-embedding process would, it registers every task itself (mirroring `./app scheduler`'s own registration, including plugin-registered cron jobs) and fires the requested one directly, then exits once it's done. This means CLI trigger always works where HTTP trigger against production `serve` doesn't — that's by design (see the source comment on `runScheduleTrigger`), not a bug in either.
- **`schedule:trigger` pays the full plugin-bootstrap cost on every call** (`registry.InitAll`, permission registry freeze, stock syncer wiring) — the same construction `./app scheduler` does at its own startup, needed so a plugin-registered cron task can be triggered too, not just the four built-in ones. If any installed plugin's `Init()` has side effects (webhook (re-)registration, background goroutines, outbound calls), those fire on **every** `schedule:trigger` invocation, even when triggering a task unrelated to that plugin. Fine for occasional manual/ops use; think twice before wiring `schedule:trigger` into a tight automated loop (e.g. a CI step that runs on every commit) if any installed plugin does non-idempotent work in `Init()`.

## Cache cleanup (`cache.cleanup`)

Runs every 5 minutes (`*/5 * * * *`) via the scheduler → job queue → worker, same as the other built-in recurring tasks.

- **Postgres (`cache.driver=postgres`, the default):** deletes rows from `cache` whose `expires_at` has passed, then (a separate statement) deletes `cache_tags` rows whose key no longer exists. The orphan sweep is a sequential `NOT EXISTS` scan of `cache_tags` — `idx_cache_tags_key` does **not** make it cheap; that index is for the per-key deletes in `Delete` / `DeleteByPrefix` / `CompareAndSubtract`. A large tag table can make one tick slow, but it is still one SQL statement, not a per-member loop.
- **Redis (`cache.driver=redis`):** value keys expire on their own via Redis TTL. The job is **not** a no-op: it `SCAN`s tag sets (`tag:*` under the configured key prefix) and, in pipelined batches, `EXISTS`-checks each member and `SREM`s those whose value key is already gone. Empty tag sets are deleted atomically. **Cost is O(total tag-set membership)** — a popular category tag once full-page cache is on can make a single `cache.cleanup` tick take noticeably longer than on Postgres. Watch job duration and the `deleted` field on `cache.cleanup.complete` (that count is members pruned, not value keys TTL-evicted). There is no cardinality cap; if cleanup ticks start running past the next 5-minute schedule, the tag set itself is the thing to inspect. A non-set key under the reserved `tag:*` prefix (or a transient error pruning one tag) is logged as `redis.cache.prune_tag_skipped` and skipped — the rest of the scan still runs; the job does not fail the whole tick for one bad key. If that event repeats, find and delete the offending Redis key. Purge-key recovery failures log `redis.cache.purge_expire_failed` / `redis.cache.purge_restore_failed` / `redis.cache.purge_restore_cleanup_failed` (the last one: membership was already restored onto the live tag key, but deleting the now-redundant purge key itself failed — the purge key's own TTL still reclaims it eventually).

## Search reindex (`search:reindex`)

- **After deploying PR-1079 (search visibility schema): migrate → full `search:reindex` → then enable `catalog.visibility.enforce`.** See [DEPLOYMENT.md — Meilisearch / search visibility schema (PR-1079)](docs/guides/DEPLOYMENT.md#meilisearch--search-visibility-schema-pr-1079). Migration `110` adds `products.search_visible_*` / `search_purchasable` with `DEFAULT true` (Postgres fail-open until reindex). Meili unreindexed docs lack those attrs; query filters use `(attr = true OR attr NOT EXISTS)` so missing fields also fail open — **but** enforce must stay off until reindex finishes, or Total/Facets disagree with the post-filter safety net. Meili also filters `(status = "active" OR status NOT EXISTS)` as defense in depth (fail-open until reindex writes `status`). After migrate, prefer a full reindex in a low-write period before enabling enforce. Ongoing freshness for Phase 12 visibility fields is covered by PR-1080 on-save triggers (plus the existing PR-1036/1049 stock paths).
- **`app search:reindex` runs as a background job (PR-1033), not inline in the CLI process.** It creates a `search_index_runs` row, enqueues a `search.reindex` job onto the configured queue, and returns immediately (unless `--wait`) — the actual scan-and-index work happens in whichever process runs the worker (`./app worker`, or `serve`'s embedded worker). A CLI invocation with no worker running anywhere just enqueues a job that never gets picked up; check `jobs:list --type=search.reindex` if a reindex seems to be stuck at 0 progress.
- **`--wait`** polls the run row every 500ms and blocks until the worker marks it `completed` (prints the indexed/total count) or `failed` (non-zero exit, prints the last error). SIGINT/SIGTERM while waiting exits with an error but does **not** cancel the job itself — the reindex keeps running in the background; re-run `search:reindex --wait` (or check `jobs:list`) to see it finish.
- **`jobs:list`/`jobs:show`/the jobs admin API (PR-1028/1029/1032) show the underlying `search.reindex` job's queue status only** (pending/processing/done/failed, attempts) — a stuck or failed reindex is retried/investigated through that same job admin surface as any other job. They do **not** show reindex-specific progress: `search_index_runs.processed_count`/`total_count` isn't exposed there — for that, use `search:reindex --wait` (final count only, not live) or `GET /api/v1/admin/search/reindex/{runID}` (PR-1035; live `total_count`/`processed_count`/`status`).
- **`search:reindex` (the CLI command) itself always triggers a full-table scan** — the same scope it always did. Partial/scoped reindex (explicit product/category IDs, or "changed since") is available via `POST /api/v1/admin/search/reindex` (PR-1035; a single product ID indexes synchronously, everything else is queued) — `ReindexService.Trigger` resolves any of these to a `search.ScopeProducts`/`ScopeCategories`/`ScopeSince`/`ScopeAll` value before enqueueing. A scope covering more than `search.reindex_full_scan_threshold` (default 20%, config-overridable) of the catalog is automatically substituted with a full scan by `Trigger` itself before enqueueing — visible, not silent: the run's recorded `scope_params` still shows what was requested.
- **On-save indexing covers most admin-driven catalog edits (PR-1036, PR-1080) — not every write path.** Editing a product, its price, its stock (via the admin inventory screen), its category assignment, a variant create/update, a bundle component add/remove, or a downloadable file attach/replace/remove through the admin API enqueues a scoped reindex — event-driven, debounced (`internal/application/search.IndexUpdateSubscriber`, a few seconds per product), via the same job queue/`search_index_runs` tracking as every other reindex. PR-1080 fans leaf product/stock/price/variant events out to parent bundles (`BundleParentLookup`) so dynamic-kit `HasPrice` / `QuantityPositive` stay fresh when a leaf deactivates, gains a second variant, or crosses zero stock. Linked-child assignment and download-grant issue/revoke do **not** trigger reindex (index-time Resolve uses empty assignment context; grants are post-purchase). **Still stale until a manual/scheduled `search:reindex`:** CSV `import:bundle-components` and `import:downloadable-files` do not batch-trigger (accepted gap from PR-1080; out of scope for PR-1081). **`import:products` is wired (PR-1081):** each successful create publishes `catalog.EventProductCreated`; the CLI registers `rewrite.Subscriber` on that bus (required for `/<slug>` — serve cannot see the CLI bus) and batch-Triggers created product IDs when `ReindexService` is available (same fallback log pattern as `import:stock`). Price CSV is covered indirectly — each row publishes `pricing.EventPriceUpserted`, and the subscriber fans parents out — but a huge price import still incurs one debounced event path per distinct product (same as before PR-1080).
- **Checkout, reservation-expiry release, and returns restocking now all publish `inventory.EventStockUpdated` too (PR-1049)** — closing what was previously the biggest gap in this coverage: an ordinary customer purchase (`internal/application/checkout.ReserveInventoryStep`, via `WithStockEventPublishing`), a reservation expiring and its stock being restored (`inventoryApp.ReservationExpiryHandler`, via `SetEventPublishing`), and a return being received and restocked (`internal/application/returns.Service.Receive`) all now keep the index fresh, the same as an admin-panel edit. The two **bulk** stock-mutation paths (`import:stock` CLI/CSV, and the ERP `StockSyncService.UpsertBySKU` sync) deliberately do **not** publish a per-row event — that would enqueue one reindex per row on a large import. Instead, when a `ReindexService` is successfully wired, each collects every product it actually committed during its own run, **expands that set through `BundleParentLookup` so parent kits are included (PR-1080; batched, on a timeout separate from enqueue; 10,000+ IDs skip expansion and full-scan)**, and fires one `ReindexService.Trigger(ScopeProducts{...})` call afterward (`StockImporter.WithReindex`+`WithBundleParents` / `StockSyncService.SetReindexService`+`SetBundleParents`), letting the existing full-scan-threshold/hard-cap logic (PR-1034) take over automatically if the touched set is large. That wiring is not guaranteed on every entry point: `import:stock` continues without reindexing when `newSearchReindexService` fails (logged `import.stock.reindex_service_unavailable`), and the standalone `worker` does the same when `newWorkerReindexService` fails — there is then no `Trigger` call, and the existing manual/scheduled `search:reindex` remains the fallback. Wiring a real `ReindexService` into `serve`'s embedded ERP sync path was straightforward once the queue exists; the standalone `worker` process needed the same two-step wiring `serve` itself uses (queue exists only after `setupWorker` runs, `ReindexService` needs that queue) plus `IndexUpdateSubscriber` on the worker's own bus so reservation-expiry events in that process actually enqueue a reindex — see `cmd/api/main.go`'s `runWorker`. `schedule:trigger`'s own plugin bootstrap (any named cron task, not specifically ERP sync) is not wired to a `ReindexService` at all — a deliberate, low-value-to-close gap for that one low-frequency, manual-trigger CLI path.

- See `docs/guides/DEVELOPER.md`'s "Add Custom Event Listeners" section for the exact events a plugin author needs to publish to participate in on-save indexing for whatever isn't covered above.
- If the worker process crashes mid-run, the `search_index_runs` row is left `processing` with a partial `processed_count` — it is not automatically resumed or retried. **A reconciliation sweep now runs automatically** (`search.reindex.reconcile`, cron `*/30 * * * *`, PR-1048): every 30 minutes it checks every run still `processing` after 2 hours (`ReconcileHandler`'s `staleAfter`) against its underlying `search.reindex` job (found via `payload->>'run_id'`, `jobs:show <job-id>`). If that job has already reached a real terminal status (`done`/`failed`/`cancelled`), **or no such job exists at all** (e.g. `ReindexService.Trigger`'s `Create` succeeded but `Enqueue` never did), the sweep corrects the run to `failed`, recording why in `last_error` — in both cases nothing will ever call `Finish` for that run again. It deliberately does **not** touch a run whose job is *found* still `pending`/`processing`: job status alone can't distinguish a genuinely orphaned job (the crashed-worker case — there is no lease/heartbeat on the `jobs` table, so a job a worker was handling when it died stays `processing` forever too, with nothing to ever reclaim it) from one still legitimately running or retrying — that case is left `processing` for an operator to judge manually (see below). Run `schedule:trigger search.reindex.reconcile` to fire the sweep immediately instead of waiting for the next tick.
- **For the ambiguous case the sweep leaves alone** (the run's underlying job is *found* but still `processing`/`pending`) — first confirm independently that the job is never coming back (e.g. the worker process that held it is confirmed dead/restarted, and no reindex is actually progressing), then run `app search:reindex-runs:reconcile <run-id> --reason="<why>"` to flip that one run to `failed` with your reason recorded in `last_error`. It refuses (409) if the run isn't currently `processing`, and requires `--reason`. This is the supported alternative to editing `search_index_runs` directly in the database.
- **The sweep is race-safe and bounded.** Marking a run `failed` is a conditional write (only a still-`processing` run is touched) — if the run finished concurrently between the sweep reading it and acting on it (e.g. an admin ran `jobs:retry` on the same job and it went on to complete), the sweep's now-stale decision is silently dropped rather than clobbering the real outcome; same guard applies to the manual CLI command. Each tick checks at most 500 stale runs within a 5-minute budget — a mass-orphan event (many workers crashing at once) is drained across however many 30-minute ticks it takes rather than one tick running unbounded. A persistent failure (not the benign "already finished concurrently" case — a genuine connectivity/permissions problem checking or correcting runs) makes the `search.reindex.reconcile` job itself fail and retry, visible via `jobs:list --type=search.reindex.reconcile`, instead of silently reporting `done` while doing nothing.
- **The automatic sweep is Postgres-queue-only** (`queue.driver=postgres`), the same constraint `jobs:list`/`jobs:show`/`jobs:retry`/`jobs:cancel` already have (job introspection has no equivalent for a broker-backed queue — see this file's own note on those commands). It looks up a run's underlying job by querying the `jobs` table directly, which is only ever populated when Postgres is the actual queue backend; on `redis`/`rabbitmq`/`kafka`/`sqs`, that table stays empty regardless of what's really running, so the handler and its cron registration are skipped entirely rather than risk reporting every live job as "missing" and failing its run out from under it. **On a broker-backed queue, only the manual CLI path applies** — `search:reindex-runs:reconcile` never reads the `jobs` table, so it works the same regardless of queue driver.
- **A run is only marked `failed` once the underlying job has exhausted its retries** (`DefaultMaxRetries = 3`, with backoff) — a transient error on attempt 1 or 2 is logged (`search.reindex.attempt_failed_will_retry`) but leaves the run `processing` for the next attempt, since the job may still go on to succeed. `--wait`/`jobs:show <job-id>` may show a few retry log lines before either side settles; that's expected, not a stuck run.
- **No snapshot isolation across a run's product scan.** The old CLI-inline reindex wrapped its whole scan in one `REPEATABLE READ` transaction; the job-based version scans in independent batches with no shared snapshot, so a product created or deleted concurrently during a reindex can be skipped or (less likely) double-indexed — the usual offset-pagination-under-concurrent-writes caveat. This is a deliberate, accepted trade-off (a transaction held open for an entire catalog scan, now potentially spanning several worker poll ticks, is its own risk — it blocks autovacuum for as long as it's open), not an oversight. If this matters for your catalog's write volume during reindexes, prefer running `search:reindex` during a low-write window.

## Liveness vs readiness

| Probe | Endpoint | Use |
| --- | --- | --- |
| Liveness | `GET`/`HEAD` `/healthz` | Process up (static 200, `Cache-Control: no-store`). Docker image `HEALTHCHECK` stays here. Served **outside** store/auth middleware. |
| Readiness | `GET`/`HEAD` `/readyz` | DB `PingContext` within ~2s → 200; else **503**. Dedicated per-IP probe rate limit (defaults match HTTP rate limit). Point load balancers / k8s readiness here. Prefer network policy so probes are not internet-public. |

If `/readyz` returns 503 while `/healthz` is 200, the API process is up but cannot reach Postgres (or the ping timed out).

## Metrics (Prometheus)

- **Disabled by default** (`metrics.enabled: false`). No `/metrics` listener starts at all — enabling nothing costs nothing.
- When enabled, `/metrics` is served on a **dedicated listener** (`metrics.listen`, default `127.0.0.1:9090`) — not on the main app port. It is never merged into the public API surface or its middleware stack (no rate limit, no auth, no CORS).
- **This endpoint has no built-in authentication.** The loopback-only default is the safety net: only change `metrics.listen` to a non-loopback address if the scrape path stays on a private network a scraper (e.g. Prometheus) can reach but the public internet cannot (Docker/Kubernetes internal network, VPN, or a reverse-proxy scrape rule with its own auth). Never bind it to a public interface directly.
- Both `serve`/`dev` and standalone `worker` processes expose `/metrics` when enabled — each process only reports the metrics it can observe (the worker process has no HTTP requests to report; the API process still reports job failures for jobs enqueued from HTTP paths).
- **Colocated serve + worker:** both would otherwise default to `127.0.0.1:9090`. The worker process automatically shifts to `127.0.0.1:9091` when it detects `metrics.listen` is still the unmodified default, so the common case needs no manual configuration. Set `metrics.listen` explicitly on both processes (via `SHOPANDA_METRICS_LISTEN`) if you want different addresses/ports than that. Bind failures fail **startup** (not a silent async log).
- The metrics `http.Server` sets the same `ReadTimeout`/`WriteTimeout`/`IdleTimeout` as the main server (10s/30s/60s) plus a 5s `ReadHeaderTimeout`, since this listener has no auth of its own.
- **Production bind policy:** startup rejects `metrics.listen` on all interfaces (`0.0.0.0`, `::`) and on non-loopback addresses unless `SHOPANDA_DEV_MODE` **or** `SHOPANDA_METRICS_ALLOW_INSECURE_BIND` is truthy. The two are independent on purpose — enabling external Prometheus scraping should not also weaken the DB password/SSL checks that `SHOPANDA_DEV_MODE` gates. Default loopback + local scraper is the supported production path.
- The `method` label on `shopanda_http_requests_total`/`_duration_seconds` is bounded to `GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS`; anything else (malformed or attacker-supplied) is recorded as `other`.
- Env overrides: `SHOPANDA_METRICS_ENABLED`, `SHOPANDA_METRICS_LISTEN`, `SHOPANDA_METRICS_ALLOW_INSECURE_BIND`.

**Metrics exposed** (all labels are bounded — fixed enums, route templates, or compile-time job types; never raw URLs, IDs, or emails):

| Metric | Type | Labels | Notes |
| --- | --- | --- | --- |
| `shopanda_http_requests_total` | counter | `route`, `method`, `status_class` | `route` is the matched route **template** (e.g. `GET /api/v1/products/{id}`), not the raw path. `status_class` is `2xx`/`3xx`/`4xx`/`5xx`/`other`, not the numeric code. Unmatched requests (404s) use the fixed label `unmatched`. |
| `shopanda_http_request_duration_seconds` | histogram | `route`, `method` | Same bounded route/method labels. |
| `shopanda_checkout_result_total` | counter | `outcome` (`success`/`failed`/`succeeded_event_failed`) | One increment per `checkout.Workflow.Execute` call (panics count as `failed`). `succeeded_event_failed` is every step succeeding but the final `checkout.completed` event publish failing — a downstream/event-bus problem on an otherwise-successful order, not a checkout failure. |
| `shopanda_job_failures_total` | counter | `job_type` | Incremented on handler error or "handler not found"; not incremented on success. |
| `shopanda_webhook_deliveries_total` | counter | `outcome` (`success`/`failed`) | Skipped deliveries (inactive/unsubscribed endpoint, malformed job payload) are not counted — they were never attempted. |
| `shopanda_ratelimit_backend_errors_total` | counter | `limiter` (`default` or `route:<path_prefix>`), `reason` (`error`, `circuit_open`, or `pool_timeout`) | Incremented on a Redis limiter error, a pool wait timeout, and on each request while the circuit is open (`reason=circuit_open`). One factory-wide outage is spread across limiter names — **sum over `limiter`**. Not incremented on a normal 429 or on a client disconnect. |
| `shopanda_fpc_requests_total` | counter | `route`, `outcome` (`hit`/`miss`/`bypass`) | `route` is an FPC allowlist template (e.g. `/products/{slug}`), never a raw path. Counted only for requests that enter the FPC wrapper. Terminal disposition only — backend Get failures use `shopanda_fpc_backend_get_errors_total`, not an outcome label. |
| `shopanda_fpc_render_duration_seconds` | histogram | `route` | Miss-path render time (what a hit avoids). |
| `shopanda_fpc_purge_total` | counter | `trigger` (`tag_invalidation`/`manual_url`/`unknown`) | **Keys deleted**, not soft-TTL observations. Soft expiry deletes the key and surfaces as a miss. Scrape serve **and** worker — tag invalidation often runs on the worker bus. |
| `shopanda_fpc_backend_get_errors_total` | counter | _(none)_ | FPC requests that observed a cache backend Get failure (at most one increment per request). Prefer this over the process-local admin `backend_get_errors` for historical/cluster rates. |

**Example Grafana/PromQL queries** (no dashboards ship — build your own from these):

```promql
# Error rate by route (RED: errors)
sum(rate(shopanda_http_requests_total{status_class="5xx"}[5m])) by (route)
  / sum(rate(shopanda_http_requests_total[5m])) by (route)

# p95 latency by route (RED: duration)
histogram_quantile(0.95, sum(rate(shopanda_http_request_duration_seconds_bucket[5m])) by (le, route))

# Checkout failure rate
sum(rate(shopanda_checkout_result_total{outcome="failed"}[5m]))
  / sum(rate(shopanda_checkout_result_total[5m]))

# Job failures by type
sum(rate(shopanda_job_failures_total[15m])) by (job_type)

# FPC hit rate by route template (exclude bypass from denominator)
sum(rate(shopanda_fpc_requests_total{outcome="hit"}[5m])) by (route)
  / sum(rate(shopanda_fpc_requests_total{outcome=~"hit|miss"}[5m])) by (route)

# FPC miss render p95
histogram_quantile(0.95, sum(rate(shopanda_fpc_render_duration_seconds_bucket[5m])) by (le, route))

# FPC keys deleted by trigger (sum across serve+worker scrapes)
sum(rate(shopanda_fpc_purge_total[15m])) by (trigger)

# FPC backend Get failures (sum across serve scrapes)
sum(rate(shopanda_fpc_backend_get_errors_total[5m]))
```

**Historical note:** the OpenTelemetry tracing gap noted here through PR-1023 is closed by PR-1024 — see the next section.

## Tracing (OpenTelemetry)

- **Disabled by default** (`tracing.enabled: false`). No exporter, no background export goroutine, no cost — the same "enabling nothing costs nothing" posture as metrics.
- When enabled, spans export via **OTLP/HTTP** to `tracing.endpoint` (host:port, no scheme — e.g. `localhost:4318`, not `http://localhost:4318`; a scheme prefix is rejected at startup, not left to fail at first export).
- **Two spans per checkout, one per HTTP request:** every HTTP request gets a span named `HTTP {method}` with `http.route` (the matched template, e.g. `/products/{id}` — never the raw URL) and `http.response.status_code` attributes. Every `checkout.Workflow.Execute` call gets a root span `checkout.execute` plus one child span per step (`checkout.step.<name>`), each with its own error status on failure.
- **No raw URLs or path segments in span attributes** — same reasoning as the metrics label policy above, applied to what leaves the process via OTLP instead of via `/metrics`. A customer ID, order ID, or reset token embedded in a path never gets exported.
- **`tracing.insecure: true` (cleartext OTLP export) is validated, not just discouraged in a comment.** Startup rejects it unless `SHOPANDA_DEV_MODE` is truthy or the endpoint resolves to a loopback host — same posture as `database.sslmode=disable`. Use TLS (the default) against anything that isn't a same-host/local collector.
- **Sampling:** `tracing.sample_ratio` (0.0–1.0) defaults to `1.0` (sample everything) when left unset. Setting it to `0` is a deliberate "instrument the code, export nothing" state, not the same as leaving it unset — both survive distinctly through config loading. Values above `1` are clamped down to `1` (typo defense — `150` is far more likely to mean "150%, i.e. everything" than a real attempt to oversample); negative values are rejected at startup.
- **Headers** (`tracing.headers`, a map) are sent with every OTLP export request — use this for a collector API key (Grafana Cloud, Honeycomb, etc. commonly require one). Redacted (`***`) wherever config values are surfaced through `Get()`/`GetOrDefault()`.
- Both `serve` and standalone `worker` processes call the same `tracing.Setup`, tagged with a distinct `service.name` resource attribute (`shopanda-api` / `shopanda-worker`) so a collector can tell them apart. The worker process doesn't currently instrument any job handler — `Setup` is wired for parity and future use, not because job spans exist yet.
- Env overrides: `SHOPANDA_TRACING_ENABLED`, `SHOPANDA_TRACING_ENDPOINT`, `SHOPANDA_TRACING_INSECURE`, `SHOPANDA_TRACING_SAMPLE_RATIO`. No env mapping for `headers` (a map) — set it in YAML.
- **Local collector (Jaeger):**
  ```bash
  docker run -d --name jaeger -p 16686:16686 -p 4318:4318 jaegertracing/all-in-one:latest
  ```
  Then set `tracing.enabled: true`, `tracing.endpoint: "localhost:4318"`, `tracing.insecure: true` (loopback, so this is allowed without `SHOPANDA_DEV_MODE`) — traces appear at `http://localhost:16686`.
- **Grafana Cloud (or any TLS OTLP/HTTP collector requiring an API key):** `tracing.endpoint: "otlp-gateway-<region>.grafana.net:443"`, `tracing.insecure: false` (the default), `tracing.headers: {Authorization: "Basic <base64 instance-id:api-key>"}`. Consult your collector's OTLP/HTTP ingest docs for the exact header format — this varies by vendor.
- **Known limitation:** DB query spans are not implemented (see PR-1024's "Scope decisions" — no shared `*sql.DB` access wrapper exists yet to hang them on) and no job/worker-level span exists yet.

## Container releases (GHCR)

Version tags (`v*`) publish `ghcr.io/<owner>/shopanda`. **Pin deploys** to `sha-<full40gitsha>` or `@sha256:…` digest — not the version tag alias. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md#pull-from-ghcr-releases).

The `sha-<commit>` tag is never overwritten: the Release workflow refuses to re-push it if it already exists in GHCR (rebuilding the same commit can still yield different image content, since Alpine packages are installed unpinned at build time). A release re-run for an already-released commit fails fast in the "Guard — sha tag must stay immutable" step — this is expected; cut a new commit/tag if you need to publish a fix.

## Supply chain

- Dependabot: weekly PRs for Go modules, Actions, and Docker digests (`.github/dependabot.yml`).
- `CI / govuln`: pinned fail-closed `govulncheck`; exceptions only via [`GOVULN_BASELINE.md`](docs/phase-10-platform-excellence/GOVULN_BASELINE.md). Details in [DEVELOPER.md](docs/guides/DEVELOPER.md#supply-chain-dependabot--govulncheck).

## Outbound webhooks (SSRF)

- Endpoint URLs must be **https**. Loopback, RFC1918, link-local (including cloud metadata `169.254.169.254`), IPv6 ULA, IANA special-purpose IPv4 (CGNAT, TEST-NET, benchmarking `198.18/15`, reserved `240/4`, …), and well-known NAT64 (`64:ff9b::/96`) destinations are rejected at create/update (literal IPs) and again at delivery (DNS resolution). Non-canonical IP hosts (`127.1`, decimal/hex forms) are rejected at create/update.
- Delivery dials only addresses that pass the allow check after resolve; if **any** A/AAAA record is disallowed, delivery fails (DNS-rebinding safe). Redirects remain disabled. `HTTP_PROXY` / `HTTPS_PROXY` are **not** honored for webhook delivery (would bypass destination IP checks).
- Admin API returns validation errors for blocked URLs; failed deliveries surface `ssrf: …` / `webhook post: …` in job errors and worker logs.

## HTTP body limits and security headers

- Default request body cap is **1 MiB** (`http.max_body_bytes` / `SHOPANDA_HTTP_MAX_BODY_BYTES`). Oversized JSON/API bodies return **413** with `error.code=payload_too_large`.
- Admin media uploads (`POST /api/v1/admin/media`, `.../upload`) use **10 MiB** by default (`http.media_max_body_bytes`). The media cap is independent of the JSON default (raising `max_body_bytes` does not raise media).
- Every response sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`. HSTS is added only on TLS or trusted-proxy HTTPS.

## Checkout cancel and timeouts

Checkout uses the HTTP request context. Client disconnects and the server **WriteTimeout** (30s, not configurable — see [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md#http-boundary)) cancel in-flight catalog, pricing, reserve, and PSP **initiate** calls. Logs: `checkout.step.failed` with a context cancel/deadline error.

Compensating work is **detached** (~30s bound) so cancel does not skip it:

| Already committed | Still runs after cancel |
| --- | --- |
| Inventory reserved for earlier cart lines | `Release` those reservations |
| Store credit redeemed, then order save/apply failed | Re-`Issue` the credit |
| PSP already accepted/captured | Persist payment status (avoids a retry minting a new payment ID / double charge) |
| Order saved | Copy cart-item extension snapshots onto the order |

If a shopper closes the tab after the PSP succeeds, you may still see a completed payment and a pending/paid order; do not treat a 499/504 at the proxy as “no charge.” Inventory leftover after a failed checkout stops counting against availability immediately (stock queries filter `expires_at > now()` at read time) even if a detached release itself failed — but the reservation row itself only actually flips to `released` status via a scheduled job (`inventory.reservation_expiry`, every 15 minutes, matching the reservation TTL) that sweeps everything expired as of the sweep time. Between expiry and the next sweep, the row still reads `active` even though it no longer reserves anything — don't treat `active` status alone as proof a reservation is still live; check `expires_at` too. On a deployment upgrading from before this job existed, the sweep also picks up the entire historical backlog of already-expired rows (the query has no "only new" filter), not just newly-expired ones. It processes in bounded batches (500 rows/transaction, `FOR UPDATE SKIP LOCKED`) so a large backlog never holds locks on `stock`/`reservations` rows for more than one batch's duration — concurrent checkout `Reserve` calls on the same variants are not blocked for the sweep's full length. Each invocation is itself capped at 10 minutes; if a backlog is large enough that one tick doesn't finish, it logs `more_remaining: true` and picks up where it left off on the next scheduled tick 15 minutes later — expect several ticks, not one long one, to fully drain a very large backlog.

Plugin authors: [PLUGIN_COMPOSITION.md](docs/guides/PLUGIN_COMPOSITION.md) (forward `ctx` vs detached compensate/persist).

## Event bus drain (SIGTERM)

After HTTP connections drain, `serve` stops the embedded scheduler and job worker and **drains async event handlers**, waited **in parallel**. Handler budget is **10 seconds** (`Drain`); `ShutdownBackground` waits **11 seconds** (10s + 1s slack) so `event.bus.drain.timeout` can be logged before the process exits. Event-bus policy is **wait-then-cancel** (not cancel at t=0):

1. Stop starting new `OnAsync` goroutines. In-flight `Publish` still runs **sync** handlers (errors still abort that publish); it does not fail only because the bus is draining.
2. Wait for in-flight `OnAsync` handlers with a **live** context for most of the Drain budget (~9s). Core handlers (order confirmation, webhook enqueue) do a DB lookup + queue insert — they should finish here.
3. Cancel remaining handlers, then wait the last ~1s so they can return. If they still outlive the 10s Drain budget, log `event.bus.drain.timeout` (and `background.shutdown.timeout` if scheduler/worker also overran). The process then exits; remaining handlers are abandoned.

The grace/cancel split is **hardcoded**, not configurable: remainder is `min(DrainTimeout/5, 1s)` (so 10s → 9s grace + 1s after cancel). Plugin authors who need a longer live-context window must keep `OnAsync` bodies short or enqueue a job; there is no env/config override.

`OnAsync` does **not** use the HTTP request context — a shopper abort does not cancel webhooks/notifications already dispatched. Plugin handlers should use the received `ctx` for DB/HTTP/queue calls so a straggler after grace does not block exit.

**Limitation:** there is no outbox/retry. Work that is still in the handler when grace ends is cancelled and not retried (same as a crash). Keep `OnAsync` bodies short (enqueue a job, don’t call the PSP inline). Ignoring `ctx` after cancel can still delay or drop the last second of shutdown.

### Customer cannot download a digital product

**Symptom:** `POST /api/v1/account/downloads/{grantID}` returns 401 / 403 / 404 / 422 / 409 / 500, or the customer says the link “does nothing.” (Delivery is **POST**, not GET — prefetch must not burn slots.)

**Check:**
```bash
# Grant row (must exist; customer_id must match the logged-in customer)
psql "$DATABASE_URL" -c "select id, customer_id, expires_at, download_limit, download_count, asset_id from download_grants where id = '<grant-id>';"

# Live vs expired: expires_at <= now() means revoked or timed out (PR-1075 revoke sets expires_at = now)
# Exhausted: download_count >= download_limit → 422 validation ("download limit reached")
# Wrong / empty customer_id → 403 (guest grants until account-link rebind)
# Asset path: prefer private/downloads/… for grant targets (upload with private_download=1)
psql "$DATABASE_URL" -c "select id, path, filename from assets where id = '<asset-id>';"
```

**Fix:**
| HTTP | Cause | Operator action |
| --- | --- | --- |
| 401 | Not signed in | Customer must authenticate |
| 403 | Grant owned by another customer, or guest `customer_id` empty | Confirm account; guest rebind is a follow-up (PR-1075) — do not invent a re-issue without paid/link context |
| 404 | Unknown grant id, missing asset row, or missing blob (Head/Read miss) | Confirm grant + `assets` + object in storage; missing blob does **not** increment `download_count` |
| 422 | Expired or download limit reached | Support: open the order or customer detail → Download grants → **Re-issue** (or `POST /api/v1/admin/download-grants/{id}/reissue` with `expiry_days`/`expires_at` + `reason`; defaults to reset count). Requires `downloads.write`. Do not rely on paid redelivery — it will not resurrect a revoked grant |
| 409 | Concurrent atomic claim race | Customer retries once |
| 500 | Presign / storage failure | Check API logs for `download delivery … failed`; do not hand out permanent `/media/...` URLs |

Delivery never returns a permanent public media URL (local streams; S3 uses a short-lived signed redirect with `private, no-store`, Location host-restricted to AWS path/virtual-hosted or MinIO endpoint — not CDN BaseURL alone). New Attach rejects non-`private/downloads/` assets; legacy `uploads/` joins remain deliverable until re-uploaded privately. **Residual risk:** keep `private/downloads/` off public `/media` aliases. See [DEVELOPER.md — Storefront download delivery](docs/guides/DEVELOPER.md#storefront-download-delivery-pr-1077).

## Common references

- [EU compliance fields](docs/phase-5-maturity/specs/COMPLIANCE_EU.md) — Omnibus, WEEE, EPR, GPSR
- Omnibus reads catalog `price_history` for the SKU on the page. Exploding a kit onto order lines does not change that, and checkout does not `Record`. History starts at the next CSV import or admin price PUT. Older admin edits are not backfilled.

| Surface | History used |
| --- | --- |
| Fixed kit PDP/PLP | Parent variant's `price_history` |
| Dynamic kit PDP/PLP | No block (the headline sum is not one SKU's prior-30-day price) |
| Leaf / component PDP/PLP | That variant's `price_history` |
| Admin price PUT and CSV price import | Append a snapshot for the variant just written. PUT fails closed if the history repo is unwired. |
- [Runtime modes](docs/phase-4-refactoring/specs/RUNTIME_MODES.md) — `serve`, `worker`, `scheduler`, `app dev`
- [Commercial B2B module](docs/COMMERCIAL.md) — license-gated `plugins/b2b`
