#!/usr/bin/env bash
# Portable unit quality contract (Phase 17).
# Same checks as CI / unit (minus runner setup). Requires: go; Node 22+ on PATH
# for TestCacheAdminUI_Node (node --test).
#
# Usage:
#   bash scripts/ci/unit.sh
#   make check
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

export GOFLAGS="${GOFLAGS:--mod=readonly}"

echo "==> go mod verify"
go mod verify

echo "==> gofmt"
files="$(gofmt -l .)"
if [[ -n "${files}" ]]; then
  echo "gofmt needed on:" >&2
  echo "${files}" >&2
  exit 1
fi

echo "==> go vet"
go vet ./...

echo "==> go test ./..."
go test ./...

# Phase 17 portable-script / product-init regressions (present on Shopanda trees).
# Do NOT auto-set SHOPANDA_PLATFORM here — product clones keep the promote fixture
# and must be able to scaffold plugins without reserved-slugs covering them.
# Live upstream CI sets SHOPANDA_PLATFORM=1 on the unit job only.
if [[ -f "${ROOT}/scripts/product-init/product-init_test.sh" ]]; then
  echo "==> phase17 script tests"
  bash "${ROOT}/scripts/product-init/product-init_test.sh"
  bash "${ROOT}/scripts/ci/docs-policy_test.sh"
fi

echo "unit OK"
