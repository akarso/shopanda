# Shopanda portable quality entrypoints (Phase 17).
# No git hooks are installed by this Makefile — run checks explicitly.
#
#   make check          # unit contract (same as CI / unit body)
#   make integration    # requires SHOPANDA_TEST_DSN
#   make govuln         # requires govulncheck on PATH
#   make docs-policy    # product-doc links + promote fixture

.PHONY: check unit integration govuln docs-policy product-init-test phase17-scripts-test sync-seed

check unit:
	bash scripts/ci/unit.sh

integration:
	bash scripts/ci/integration.sh

govuln:
	bash scripts/ci/govuln.sh

# Platform default. On product trees without the promote fixture, run:
#   bash scripts/ci/docs-policy.sh
# (leave SHOPANDA_PLATFORM unset).
docs-policy:
	SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh

sync-seed:
	bash scripts/product-init/sync-seed.sh

product-init-test:
	bash scripts/product-init/product-init_test.sh

phase17-scripts-test: product-init-test
	bash scripts/ci/docs-policy_test.sh
