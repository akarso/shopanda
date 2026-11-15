# Appearance — specification

WordPress-like **Themes** + **Site Identity**. Theme files stay on disk (Phase 14 Hugo model). Admin only **discovers** and **activates**, and edits branding the default theme already knows how to render.

## 1. Why now

[THEME_SYSTEM v1](../../phase-1-core/specs/THEME_SYSTEM.md) listed “no multi-theme switching / preview” as a non-goal. That blocked merchants: logo and tagline are hardcoded in `themes/default/templates/_header.html`, and switching themes is `frontend.theme_path` / `SHOPANDA_FRONTEND_THEME_PATH`.

Phase 15 **supersedes the switching non-goal**. Preview iframe and ZIP upload stay out.

## 2. Theme catalog

Scan `{themes_dir}/*/theme.yaml` (default dir: `themes/`). Each entry:

| Field | Source |
| --- | --- |
| `code` | Directory name (`default`, `my-child`) |
| `name` | `theme.yaml` `name` |
| `parent` | `theme.yaml` `parent` (already supported) |
| `screenshot` | Optional `screenshot.png` in the theme root, served as static |

Do not embed themes in the binary. Do not recurse past one level. Invalid `theme.yaml` is skipped and logged.

## 3. Activation

Store-scoped active theme **code** (directory name), not a full path.

Resolution:

```text
1. Store active theme code → {themes_dir}/{code}
2. Else frontend.theme_path (current deploy default: themes/default)
```

Config additions (closeout writes the example):

| Key | Meaning |
| --- | --- |
| `frontend.themes_dir` | Catalog root (default `themes`) |
| `frontend.theme_path` | Fallback path when no store activation exists |

Admin API:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/themes` | Catalog + which code is active for the current store |
| PUT | `/api/v1/admin/themes/active` | `{ "code": "default" }` — 422 if unknown |

Activating an unknown code is rejected. Missing directory after activate (operator deleted the folder) falls back to `theme_path` and surfaces a warning on the Themes page.

Dev-mode watch (PR-1102) keeps watching the resolved path.

## 4. Admin IA (WordPress analogue)

| Route | Nav | WP analogue |
| --- | --- | --- |
| `/admin/appearance/themes` | Appearance → Themes | Appearance → Themes (Activate) |
| `/admin/appearance/site-identity` | Appearance → Site Identity | Customizer → Site Identity (form only) |

New **Appearance** nav group. No item ships until its UI PR (Phase 6 nav policy).

## 5. Site identity (branding)

Per store. Not legal identity.

| Field | Notes |
| --- | --- |
| `title` | Storefront `<title>` / header. Empty → `store.Name` → `theme.yaml` name (current fallback). |
| `tagline` | Header `<small>`. Empty → theme default or omit. |
| `logo_media_id` | Existing media library upload. Public URL derived. |
| `favicon_media_id` | Same. |

Replaces the Settings → General **Logo URL** text field. Backfill: if `store.logo` looks like a URL and no media id is set, keep serving that URL until the merchant uploads.

Admin API: `GET/PUT /api/v1/admin/site-identity`. Public identity GET (Track A) may include `title`, `tagline`, `logo_url`, `favicon_url` once this track lands — or a dedicated `GET /api/v1/store/appearance`. Prefer extending public identity in PR-1222 so headless has one storefront bootstrap.

## 6. Default theme consume (PR-1223)

`themes/default` and `themes/_starter`:

- Header mark: `<img>` when `logo_url` set, else current “S” fallback
- `<strong>` = site title
- `<small>` = tagline when set; omit when empty (no hardcoded “Merchant-ready storefront”)
- `<link rel="icon">` when favicon set

Child themes inherit via existing partials. Document the layout fields in THEME_SLOTS / theme author guide during PR-1240.

## 7. Non-goals

* Live preview / “Customizer” iframe
* Theme ZIP upload or marketplace
* Per-theme custom CSS editor (Phase 7 snippets stay the extension path)
* Color / typography token UI
* Magento layout XML
* Multi-theme preview query param (`?theme=`)

## 8. C4

ThemeEngine already exists. Add ThemeCatalog + store-scoped activation read in the application layer. Update `c4-component.md` in PR-1220 or PR-1240. No new container.
