# Store identity — specification

Magento-like **Store Information** and **store addresses**, without Magento’s country-specific schema sprawl.

## 1. Why a domain, not more config keys

Today `store.address` and `store.logo` are store-scoped config strings. Invoices, email layout, the footer, and GPSR-adjacent imprint all need **typed** fields. A blob cannot be split into street / postcode / VAT line.

`store.Store` stays the commerce context (code, name, currency, country, language, domain). Identity is a sibling aggregate keyed by `store_id`.

## 2. Aggregates

### 2.1 Identity

Per store, at most one row.

| Field | Notes |
| --- | --- |
| `legal_name` | Company name on invoices / imprint. Not `store.Name` (doing-business-as). |
| `phone` | General contact telephone. |
| `hours` | Free-text store hours (Magento has this; optional). |
| `email_general` | Public / imprint / contact. |
| `email_sales` | Order-related display / reply. |
| `email_support` | Customer service. |

SMTP `mail.smtp.from` remains the **envelope sender**. These three are identities shown to humans. v1 does not pick From per template (Magento “sender identity”); consumers fall back `sales → general → smtp.from`.

### 2.2 Address roles

Exactly the Magento set we need. One structured address per role per store.

| Role | Used for |
| --- | --- |
| `general` | Footer, email layout, public identity |
| `invoice` | Invoice PDF seller block; imprint. Falls back to `general` when empty. |
| `shipping_origin` | Warehouse / rate origin (data now; shipping calculator consume is a follow-up) |
| `returns` | RMA / return label address. Falls back to `shipping_origin` then `general`. |

Fields: `line1`, `line2`, `city`, `region`, `postcode`, `country` (ISO-2), optional `phone`.

No free-text blob on the new model. Backfill: existing `store.address` → `general.line1`.

### 2.3 Tax identifiers

```text
{ store_id, type, value }
```

`type` is a registry code, not a column. A store may have many. Unique `(store_id, type)`.

## 3. Tax identifier type registry

Core does **not** grow `if country == "PL"` columns. It ships a catalog; plugins add types.

```go
type IdentifierType struct {
    Code       string   // "pl_nip"
    Label      string   // "NIP"
    Countries  []string // "PL"; empty = show under "Other"
    Region     string   // "EU" | "US" | "" — grouping only
    FormatHint string   // "1234567890"
}
```

Plugin port: `app.RegisterTaxIdentifierType(IdentifierType)`. Duplicate codes rejected at boot.

### 3.1 Core catalog (v1)

| Code | Label | Countries | Region |
| --- | --- | --- | --- |
| `eu_vat` | EU VAT | EU ISO-2 list | EU |
| `pl_nip` | NIP | PL | EU |
| `de_ustidnr` | USt-IdNr. | DE | EU |
| `de_steuernummer` | Steuernummer | DE | EU |
| `fr_siret` | SIRET | FR | EU |
| `fr_tva` | N° TVA | FR | EU |
| `it_partita_iva` | Partita IVA | IT | EU |
| `es_nif` | NIF / CIF | ES | EU |
| `nl_kvk` | KVK | NL | EU |
| `nl_btw` | BTW | NL | EU |
| `uk_vat` | VAT | GB | — |
| `uk_crn` | Company number | GB | — |
| `us_ein` | EIN | US | US |
| `generic` | Tax ID | (other) | — |

Admin: types where `Countries` contains the store country, plus region matches (e.g. `eu_vat` for any EU store), plus an **Add other** list. v1 validates **non-empty** and optional max length. **Checksums are out of scope** (follow-up).

GPSR manufacturer name/contact stay on `legal.gpsr_*`. Do not auto-copy `legal_name` (hidden magic).

## 4. APIs

Admin (store-scoped via existing admin context):

| Method | Path | Purpose |
| --- | --- | --- |
| GET/PUT | `/api/v1/admin/store-identity` | Identity fields |
| GET | `/api/v1/admin/store-addresses` | All roles (empty placeholders) |
| GET/PUT | `/api/v1/admin/store-addresses/{role}` | One role |
| GET/PUT/DELETE | `/api/v1/admin/tax-identifiers` | List / upsert / delete by type |
| GET | `/api/v1/admin/tax-identifier-types` | Registry, filterable by country |

Public / storefront (no secrets):

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/store/identity` | Legal name, phone, emails, general+invoice addresses, tax IDs, hours |

Headless storefronts use the public GET. SSR reads the same application service.

## 5. Admin IA

| Route | Nav | Magento analogue |
| --- | --- | --- |
| `/admin/settings/store-information` | Settings → Store Information | Stores → Config → General → Store Information |
| `/admin/settings/store-addresses` | Settings → Store Addresses | Shipping origin + store addresses as first-class pages |
| `/admin/settings/store-addresses/{role}` | (from index) | Per-address form |

Settings → General keeps SMTP + media storage. Store name/code/domain/country/locale/currency stay on the existing store entity form (Settings or Store Management). Do not duplicate them on Store Information.

## 6. Consumers (PR-1205)

| Consumer | Fields |
| --- | --- |
| Invoice PDF | `legal_name`, invoice address (fallback general), tax IDs, phone, `email_sales` or `email_general` |
| `mail.EmailData` | `StoreName` = `store.Name`; `LogoURL` from site identity when Track C exists, else leftover `store.logo`; `StoreAddress` = formatted general address; later: pass structured identity in `Data` if templates need it |
| Storefront layout | Legal name, general address, tax ID line, phone — footer partial |

Format address as a single display string for email layout (`line1, line2, postcode city, country`) so existing `_layout.html` keeps working.

## 7. Migration / deprecation

1. Create tables; backfill `store.address` → `addresses.general.line1`.
2. Keep reading `store.address` until PR-1205; then writers go to identity only.
3. Stop exposing `store.address` on Settings → General in PR-1203. Config key may remain readable for one release.
4. `store.logo` is replaced by Site Identity (Track C), not by this spec.

## 8. C4

New application service + postgres repo + admin handlers. Update `docs/diagrams/c4-component.md` in PR-1202 or PR-1240. No new container.
