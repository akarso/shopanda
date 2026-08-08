# Phase 10 — Platform Excellence

## Strategy

* Reach **A** (trustworthy) then **A+** (production-grade) from the 2026-08 tech audit baseline
* **Refactor, do not rewrite** — keep domain, hexagon, and the existing test corpus
* One PR = one responsibility; runnable, testable, reviewable in ~10–20 minutes
* Machine-enforced discipline over documentation theater (CI first)
* PR specs live under `prs/` (**PR-1000+**)

Each PR is tagged **`[oss]`** unless noted.

---

## Audit baseline (starting point)

| Area | Grade | Headline |
| --- | --- | --- |
| Architecture | B | Real hexagon; fictional plugin isolation; 2,900-line `main.go` |
| Code quality | B− | Strong conventions; suite red; no metrics; binary in git |
| Security | B code / C defaults | No Critical; High = unlimited login by default |
| Infra / CI-CD | D+ | Solid Docker/docs; **zero CI**; lying `/healthz` |

Evidence summary: ~165k LOC Go, ~400 test files, `go build` green, `go test` fails in 6 packages, no `.github/workflows`, 50MB `api` binary tracked (~2GB `.git`).

---

## Target grades

### A — exit criteria (Tracks A–D)

| Pillar | Must be true |
| --- | --- |
| Process | CI runs `gofmt` / `go vet` / `go test ./...` (+ Postgres integration job); **required status checks** on `main`/`dev` (admin ruleset) so red checks cannot merge |
| Hygiene | `api` binary not tracked; `.git` cleaned; gofmt clean |
| Security | Rate limit on by default + login lockout; JWT secret ≥32 bytes; body size caps; security headers; webhook SSRF guard; no `changeme` / weak compose defaults for prod-shaped layout |
| Ops | `/readyz` pings DB; image publish on tag; migration prefix uniqueness checked in CI |
| Architecture | `main.go` decomposed; typed plugin providers; no global RBAC mutation; checkout steps take `context.Context`; async events drain on shutdown |
| Honesty | Plugin isolation claim matches enforcement **or** docs stop claiming third-party isolation |

### A+ — exit criteria (Track E on top of A)

| Pillar | Must be true |
| --- | --- |
| Observability | Prometheus `/metrics` (RED + checkout/queue/webhook) + basic OpenTelemetry traces |
| Boundaries | Plugin lint forbids `plugins → internal/{infrastructure,interfaces}`; SDK path documented |
| Structure | `interfaces/http` split into admin / storefront / shared packages |
| Data | `pgx` migration plan executed (or staged behind driver flag with CI coverage) |
| Guidance | Extension-point decision guide (events vs hooks vs pipelines vs workflows) |
| Ops | Documented rollback/backup playbook; Dependabot + `govulncheck` green |

---

## Tracks

| Track | Goal | PR range | Moves grade |
| --- | --- | --- | --- |
| **A** | Safety net | PR-1000–1003 | B− → B+ process |
| **B** | Security hardening | PR-1004–1008 | Security → A |
| **C** | Ops / CI-CD | PR-1009–1012 | Infra → A− |
| **D** | Architecture refactor | PR-1013–1019 | Architecture → A |
| **E** | Excellence | PR-1020–1026 | All → **A+** |

**Hard rule:** do not start Track D/E until Track A CI is green on `main`/`dev`. Security (B) may overlap late A once the suite compiles.

Recommended order:

```text
1000 → 1001 → 1002 → 1003
         ↓
1004 ∥ 1005 → 1006 → 1007 → 1008
         ↓
1009 → 1010 → 1011 → 1012
         ↓
1013 → 1014 → 1015 → 1016 → 1017
1018 ∥ 1019
         ↓
1020 → 1021 → 1022 → 1023 → 1024 → 1025 → 1026
```

---

## Track A — Safety net (PR-1000–1003)

**Goal:** Make the existing test corpus a real gate. Everything else is theater until this lands.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1000 | Unbreak the test suite | Fix non-compiling mocks (`FindBySKUs`, `SetStocks`); fix RBAC permission count; fix extension-port expectation; de-flake integrationdemo HMAC timestamp test; fix mutex-by-value in webhook test |
| PR-1001 | Repo hygiene | `.gitignore`/`dockerignore` for `api`; `git rm --cached api`; gofmt all; delete unused `internal/infrastructure/postgres/migrations/022_*` (runtime uses root `migrations/`) |
| PR-1002 | GitHub Actions CI (unit) | Workflow reports `CI / unit` (`gofmt`/`vet`/`test`, `-mod=readonly`); admin must require the check to block merges |
| PR-1003 | CI integration job | `CI / integration`: Postgres 17 + readiness + `-json` anti-skip + `-p 1` — **in progress** until DSN suite green |

**Definition of done:** CI fails on broken mocks/assertions; branch rules require **`CI / unit`** and **`CI / integration`** so those failures cannot merge. Integration job must use `go test -json` anti-skip (not default text) and `-p 1`.

---

## Track B — Security hardening (PR-1004–1008)

**Goal:** Close the High finding and Medium default/HTTP gaps from the security audit.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1004 | Login abuse protection | Rate limit on by default; shared or single-instance-restricted lockout store; IP+account keys; bounded TTL |
| PR-1005 | JWT secret strength | Shared parser; ≥32 decoded bytes; accept installer 64-hex; env-named errors |
| PR-1006 | HTTP boundary hardening | Route-aware body limits; exact security-header tests; HSTS only on TLS/trusted proto |
| PR-1007 | Webhook SSRF guard | Private-IP block + mandatory DNS-rebinding-safe dial; multi A/AAAA tests |
| PR-1008 | Secure-by-default config | `changeme`/`shopanda` only if `SHOPANDA_DEV_MODE` truthy; non-local sslmode enforce; explicit reset-token log flag |

---

## Track C — Ops / CI-CD (PR-1009–1012)

**Goal:** Deployable artifacts and honest health signals.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1009 | Readiness probe | Add `/readyz` (bounded DB ping → 503); keep `/healthz` liveness + Docker `HEALTHCHECK`; `/readyz` for traffic |
| PR-1010 | Release to GHCR | On tag: push image; deploy pin = full commit SHA tag or digest; version tag is alias only |
| PR-1011 | Migration hygiene CI | Prefix uniqueness with integer normalize + exact `025_*` allowlist; no rename of applied files |
| PR-1012 | Supply-chain basics | Dependabot; pinned fail-closed `govulncheck` + baseline/exception process |

---

## Track D — Architecture refactor (PR-1013–1019)

**Goal:** Keep the hexagon; remove the localized rot that blocks an A.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1013 | Split serve wiring | Extract repo/service construction from `runServe` into `cmd/api` modules (`wire_repos.go`, `wire_services.go`); no behavior change |
| PR-1014 | Collapse import/export CLI | Shared helper for open-DB → repos → plugin registry → run; delete ~14 near-clone `runImport*`/`runExport*` bodies |
| PR-1015 | Typed plugin providers | Replace `RegisterX(provider any)` with typed interfaces; compile-time failures for wrong types |
| PR-1016 | RBAC registry injection | Single app-owned registry; same instance for Init + auth; freeze after Init; duplicate = error |
| PR-1017 | Plugin boundary honesty | Fixed allowlist (pkg/domain/application/platform); forbid infrastructure+interfaces; CI import walk |
| PR-1018 | Checkout context | Propagate request `ctx` into all blocking checkout/payment/inventory calls; cancel test |
| PR-1019 | Event bus drain | Publish barrier; WG before goroutine; cancel-then-wait shutdown policy |

---

## Track E — Excellence / A+ (PR-1020–1026)

**Goal:** Production-grade observability, structure, and long-term maintainability.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1020 | Prometheus metrics | Default off; restricted `/metrics`; bounded route-template labels (no raw URLs/IDs) |
| PR-1021 | HTTP package split (shared) | Extract shared middleware/response/auth helpers to `interfaces/http/shared` (or equiv.); no route moves yet |
| PR-1022 | HTTP package split (admin) | Move admin handlers to `interfaces/http/admin`; update wiring |
| PR-1023 | HTTP package split (storefront) | Move storefront handlers; shrink god `StorefrontHandler` deps where cheap |
| PR-1024 | OpenTelemetry traces | Optional OTLP export; instrument HTTP + checkout + DB spans behind config |
| PR-1025 | pgx driver migration | Replace maintenance-mode `lib/pq` with `jackc/pgx/v5` stdlib bridge; keep SQL; CI green |
| PR-1026 | Extension decision guide + runbook | Docs-only (approved): `EXTENSION_POINTS.md` + expand `RUNBOOK.md` |

---

## Out of scope (explicit)

| Item | Why deferred |
| --- | --- |
| Full rewrite / new framework | Architecture is salvageable |
| Runtime `.so` plugins | Already deferred by product decision |
| Down migrations for all 60+ files | Costly; Track E documents restore-based rollback instead |
| Kubernetes / Terraform | Still optional; GHCR + compose/systemd remain the path |
| New merchant features | Belongs in a product phase, not excellence |
| Retroactive codegen for all postgres repos | Leave existing boilerplate; optional later |

---

## Effort estimate (calendar, one focused engineer)

| Track | Effort | Notes |
| --- | --- | --- |
| A | 1–2 days | Blocks everything |
| B | 3–5 days | Can overlap late A |
| C | 2–3 days | After CI exists |
| D | 1.5–2.5 weeks | Pure refactor; keep behavior tests green |
| E | 2–3 weeks | A+; can stage after shipping A |

**Time to A:** ~3–4 weeks. **Time to A+:** ~5–7 weeks total.

Optional history rewrite (`git filter-repo` to purge historical `api` blobs) is an ops task outside a feature PR — schedule after PR-1001; coordinate with anyone who has clones.

---

## PR index (quick reference)

| PR | Track | Status |
| --- | --- | --- |
| 1000–1002 | A | done |
| 1003 | A | in progress |
| 1004–1005 | B | done |
| 1006–1008 | B | planned |
| 1009–1012 | C | planned |
| 1013–1019 | D | planned |
| 1020–1026 | E | planned |

PR specs: [`prs/`](prs/).

Planned-spec tightenings from the post-audit plan review are documented separately in [`PLAN_CR.md`](PLAN_CR.md) (not part of PR-1001 hygiene).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 9 | Integrator backlog + merchant discovery | Shipped (PR-856–908) |
| **Phase 10** | Platform excellence | **Planned (PR-1000+)** |
