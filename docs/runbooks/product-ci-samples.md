# Runbook: product CI samples (opt-in)

- **Introduced by:** Phase 17
- **Severity when triggered:** low (developer workflow)
- **Owner:** product maintainer

## Purpose

Enable or omit GitHub / GitLab pipelines for a product fork without inheriting
Shopanda upstream’s required workflows.

## Local-only / FTP (default)

Do nothing. Do not copy `ci-samples/`. Do not add git hooks.

```bash
make check
# deploy binary / container however you like
```

## Materialize samples into a product tree

```bash
bash scripts/product-init.sh --slug myproduct --with-ci-samples
# or, if the plugin already exists:
bash scripts/product-init.sh --slug myproduct --force --with-ci-samples
```

This refreshes `scripts/ci/` and `ci-samples/` from the product-init seed. It still does **not** enable live CI.

## Enable GitHub Actions

1. Ensure `scripts/ci/` exists in the product tree (Phase 17+ Shopanda pin, or `--with-ci-samples`).
2. Copy samples:

   ```bash
   mkdir -p .github/workflows
   cp ci-samples/github/ci.yml .github/workflows/ci.yml
   cp ci-samples/github/release.yml .github/workflows/release.yml   # optional
   ```

3. For release: set repository variable **`SHOPANDA_IMAGE_NAME`** to your product
   slug. The sample refuses to publish as `shopanda`.
4. Comment out optional jobs (e.g. docs-policy) if you do not use Shopanda’s
   `docs/phase-*` promote model.

## Enable GitLab CI

```bash
cp ci-samples/gitlab/.gitlab-ci.yml .gitlab-ci.yml
```

Adjust the Go/Node images to match your pin. Comment out docs-policy if unused.

## Disable

Delete `.github/workflows/*` samples and/or `.gitlab-ci.yml`. Portable scripts
remain available via `make check`.

## Verification

- Push a branch: sample unit job runs `bash scripts/ci/unit.sh`.
- Or locally: `make check` exits 0.
