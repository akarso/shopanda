#!/usr/bin/env bash
# DSN-gated integration suite for CI (PR-1003).
# Requires: go, jq, SHOPANDA_TEST_DSN (non-empty).
set -euo pipefail

if [[ -z "${SHOPANDA_TEST_DSN:-}" ]]; then
  echo "ERROR: SHOPANDA_TEST_DSN is empty or unset" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required to parse go test -json output" >&2
  exit 1
fi

pkgs=(
  ./internal/infrastructure/postgres/...
  ./internal/platform/migrate/...
  ./plugins/b2b/...
)

json="$(mktemp)"
trap 'rm -f "$json"' EXIT

# Serialize packages (-p 1): multiple packages call migrate.Run on the same DB.
# Capture exit without aborting the shell so we can report JSON failures.
set +e
go test -p 1 -count=1 -json "${pkgs[@]}" >"$json"
status=$?
set -e

if [[ "$status" -ne 0 ]]; then
  echo "ERROR: go test failed with exit $status" >&2
  jq -r 'select(.Action=="fail") | "FAIL \(.Package) \(.Test // "")"' "$json" >&2 || true
  exit "$status"
fi

# Any skipped test under these packages means a DSN-gated test did not run
# (or an unexpected skip). With DSN set, skip count must be zero.
skip_count="$(jq -r '[select(.Action=="skip" and .Test != null)] | length' "$json")"
if [[ "$skip_count" -ne 0 ]]; then
  echo "ERROR: $skip_count test(s) skipped while SHOPANDA_TEST_DSN is set:" >&2
  jq -r 'select(.Action=="skip" and .Test != null) | "SKIP \(.Package) \(.Test)"' "$json" >&2
  jq -r 'select(.Action=="output" and (.Output|test("SHOPANDA_TEST_DSN|--- SKIP"))) | .Output' "$json" >&2 || true
  exit 1
fi

# Canaries: known DSN-backed tests must pass (proves DB suite actually ran).
canaries=(
  "github.com/akarso/shopanda/internal/infrastructure/postgres|TestProductRepo_CreateAndFindByID"
  "github.com/akarso/shopanda/plugins/b2b/groups|TestPostgresRepo_SaveListFind"
)
for c in "${canaries[@]}"; do
  pkg="${c%%|*}"
  test="${c##*|}"
  if ! jq -e --arg p "$pkg" --arg t "$test" \
    'select(.Action=="pass" and .Package==$p and .Test==$t)' "$json" >/dev/null; then
    echo "ERROR: canary DSN test did not pass: $pkg $test" >&2
    exit 1
  fi
done

pass_tests="$(jq -r '[select(.Action=="pass" and .Test != null)] | length' "$json")"
echo "Integration suite OK: $pass_tests test(s) passed, 0 skipped (go test -p 1)."
