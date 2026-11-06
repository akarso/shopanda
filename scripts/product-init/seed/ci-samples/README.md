# CI samples for product sites

These files are **samples**, not live CI. Shopanda upstream keeps its own
[`.github/workflows`](../.github/workflows/).

`scripts/product-init.sh --with-ci-samples` copies/refreshes this directory and
`scripts/ci/` into the product tree. It does **not** install live workflows —
you still copy YAML into place deliberately.

## Modes

| Mode | What to do |
| --- | --- |
| Local-only / FTP | Do nothing. Never copy these files. No git hooks. |
| GitHub Actions | Copy `github/*.yml` into `.github/workflows/` and adjust |
| GitLab CI | Copy `gitlab/.gitlab-ci.yml` to the repo root and adjust |

All samples call [`scripts/ci/`](../scripts/ci/) — the same portable contract
Shopanda upstream uses. Action pins use full SHAs (same as upstream).

The GitLab sample installs Node from the official nodejs.org **linux-x64**
tarball with a pinned SHA-256. **arm64 runners are not covered** by that pin —
extend the sample or use a custom image on arm64.

## Enable GitHub sample

```bash
mkdir -p .github/workflows
cp ci-samples/github/ci.yml .github/workflows/ci.yml
cp ci-samples/github/release.yml .github/workflows/release.yml
# Set repository variable SHOPANDA_IMAGE_NAME to your product slug (required for release).
```

## Enable GitLab sample

```bash
cp ci-samples/gitlab/.gitlab-ci.yml .gitlab-ci.yml
```

The GitLab sample installs **jq** and **Node 22** before calling `scripts/ci/*`.

## Disable / omit

Delete the workflow files, or never copy them. `make check` / `bash scripts/ci/unit.sh`
still work locally.

See [PRODUCT_SITES.md](../docs/guides/PRODUCT_SITES.md) and
[runbooks/product-ci-samples.md](../docs/runbooks/product-ci-samples.md).
