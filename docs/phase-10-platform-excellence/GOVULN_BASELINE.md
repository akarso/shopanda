# Govulncheck baseline (PR-1012)

Fail-closed policy: CI runs a **pinned** `govulncheck` and fails on any
vulnerability that affects reachable code (`govulncheck ./...` symbol mode).

## Baseline exceptions

When a finding cannot be fixed immediately (upstream lag, coordinated
disclosure window, etc.), add a row below. **New** findings not listed here
must fail CI.

| OSV ID | Module / package | Owner | Expiry (UTC) | Notes |
| --- | --- | --- | --- | --- |
| *(none)* | — | — | — | Empty after Go 1.25.13 + `golang.org/x/image@v0.43.0` (PR-1012) |

Rules:

1. Every baseline row needs an **owner** and an **expiry** date (YYYY-MM-DD, UTC).
2. Expiry must be **≤ 90 days from today** when the row is checked in CI (enforced by `.github/scripts/run-govulncheck.sh`).
3. Expired rows are treated as failures (remove the exception or fix the vuln).
4. The fenced allowlist and the table must list the **same** OSV IDs (CI fails if they diverge).
5. Prefer upgrading the dependency / Go toolchain over extending the baseline.
6. Do not baseline findings you have not triaged.

Machine-readable allowlist used by CI (one OSV ID per line; `#` comments OK):

```text
# empty
```

## Process

1. CI fails with an unexpected OSV ID → triage (fix, upgrade, or temporary baseline).
2. To baseline: add the OSV ID to the fenced list above **and** the table (owner + expiry ≤ 90 days).
3. Open a follow-up issue/PR before expiry to remove the exception.

Pinned scanner version is recorded in [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) (`GOVULNCHECK_VERSION`).
