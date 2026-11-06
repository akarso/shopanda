# Runbook: upgrading a product fork’s Shopanda pin

- **Introduced by:** Phase 17
- **Severity when triggered:** medium
- **Owner:** product maintainer

## Remotes

| Remote | Role |
| --- | --- |
| `origin` | Product repository (push here) |
| `upstream` | `https://github.com/akarso/shopanda.git` — **fetch only** |

```bash
git remote set-url --push upstream DISABLED
```

Do **not** merge from a local Shopanda working tree.

## Procedure

1. Read upstream commits since your pin: `git fetch upstream` then review
   `UPSTREAM_PIN..upstream/main` (or chosen SHA).
2. Branch: `upgrade/shopanda-<shortsha>`.
3. `git merge <sha>` (explicit SHA until upstream tags exist).
4. Resolve conflicts:
   - **`docs/`** — keep product narrative; retain or regenerate
     `docs/GOVULN_BASELINE.md` if you run govuln; do not blindly delete
     `scripts/ci/`.
   - **`cmd/api/register_plugins.go`** — re-apply your plugin registration line.
   - **`.github/workflows`** — products using samples: re-copy from
     `ci-samples/` if upstream samples changed; do not take upstream live
     workflows unless you intend to.
   - Prefer upstream for core/engine files unless listed in your upstream
     deviations doc.
5. Update your pin document / README.
6. Validate:

   ```bash
   go build -o app ./cmd/api
   go test ./plugins/ -run ImportBoundary -count=1
   make check
   ```

7. Deploy with your usual rollback plan.

## Upstream deviations template

Keep a short table in product docs (example):

| Path | Why it differs | Since |
| --- | --- | --- |
| `README.md` | Product banner | init |
| `cmd/api/register_plugins.go` | Register `plugins/<slug>` | init |
| `.gitignore` | Product ignores | init |

See also [`guides/PRODUCT_SITES.md`](../guides/PRODUCT_SITES.md).
