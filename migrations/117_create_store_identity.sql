-- Store identity, addresses, and tax identifier values (PR-1202).

CREATE TABLE store_identities (
    store_id      TEXT PRIMARY KEY REFERENCES stores(id) ON DELETE CASCADE,
    legal_name    TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    hours         TEXT NOT NULL DEFAULT '',
    email_general TEXT NOT NULL DEFAULT '',
    email_sales   TEXT NOT NULL DEFAULT '',
    email_support TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE store_addresses (
    store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    role     TEXT NOT NULL CHECK (role IN ('general', 'invoice', 'shipping_origin', 'returns')),
    line1    TEXT NOT NULL DEFAULT '',
    line2    TEXT NOT NULL DEFAULT '',
    city     TEXT NOT NULL DEFAULT '',
    region   TEXT NOT NULL DEFAULT '',
    postcode TEXT NOT NULL DEFAULT '',
    country  TEXT NOT NULL CHECK (length(country) = 2 AND country = upper(country)),
    phone    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (store_id, role)
);

CREATE TABLE store_tax_identifiers (
    store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    type     TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (store_id, type)
);

-- Backfill general.line1 from store-scoped config store.address; country from stores.
INSERT INTO store_addresses (store_id, role, line1, country)
SELECT
    s.id,
    'general',
    left(btrim(c.value #>> '{}'), 255),
    s.country
FROM config c
JOIN stores s
  ON c.key = 'store::' || s.id || '::store.address'
WHERE jsonb_typeof(c.value) = 'string'
  AND btrim(c.value #>> '{}') <> ''
ON CONFLICT (store_id, role) DO NOTHING;
