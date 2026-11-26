-- Store identity, addresses, and tax identifier values (PR-1202).
-- Length checks mirror internal/domain/store Max* constants.

CREATE TABLE store_identities (
    store_id      TEXT PRIMARY KEY REFERENCES stores(id) ON DELETE CASCADE,
    legal_name    TEXT NOT NULL DEFAULT '' CHECK (char_length(legal_name) <= 255),
    phone         TEXT NOT NULL DEFAULT '' CHECK (char_length(phone) <= 64),
    hours         TEXT NOT NULL DEFAULT '' CHECK (char_length(hours) <= 255),
    email_general TEXT NOT NULL DEFAULT '' CHECK (char_length(email_general) <= 254),
    email_sales   TEXT NOT NULL DEFAULT '' CHECK (char_length(email_sales) <= 254),
    email_support TEXT NOT NULL DEFAULT '' CHECK (char_length(email_support) <= 254),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE store_addresses (
    store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    role     TEXT NOT NULL CHECK (role IN ('general', 'invoice', 'shipping_origin', 'returns')),
    line1    TEXT NOT NULL DEFAULT '' CHECK (char_length(line1) <= 255),
    line2    TEXT NOT NULL DEFAULT '' CHECK (char_length(line2) <= 255),
    city     TEXT NOT NULL DEFAULT '' CHECK (char_length(city) <= 255),
    region   TEXT NOT NULL DEFAULT '' CHECK (char_length(region) <= 255),
    postcode TEXT NOT NULL DEFAULT '' CHECK (char_length(postcode) <= 64),
    country  TEXT NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    phone    TEXT NOT NULL DEFAULT '' CHECK (char_length(phone) <= 64),
    PRIMARY KEY (store_id, role)
);

CREATE TABLE store_tax_identifiers (
    store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    type     TEXT NOT NULL CHECK (char_length(type) <= 64 AND type ~ '^[a-z][a-z0-9_]*$'),
    value    TEXT NOT NULL CHECK (char_length(value) <= 64 AND char_length(btrim(value)) > 0),
    PRIMARY KEY (store_id, type)
);

-- Backfill general from config with the same precedence as resolveScopedValue:
-- store::{id}::store.address when set, else bare store.address.
-- Skip rows that would fail NewAddress (bad country, over-long, controls/bidi).
-- Keep SQL in sync with postgres.StoreAddressBackfillSQL.
INSERT INTO store_addresses (store_id, role, line1, country)
SELECT
    c.store_id,
    'general',
    c.line1,
    c.country
FROM (
    SELECT
        s.id AS store_id,
        COALESCE(
            NULLIF(btrim(scoped.value #>> '{}'), ''),
            NULLIF(btrim(bare.value #>> '{}'), '')
        ) AS line1,
        CASE
            WHEN upper(btrim(s.country)) = 'UK' THEN 'GB'
            ELSE upper(btrim(s.country))
        END AS country
    FROM stores s
    LEFT JOIN config scoped
      ON scoped.key = 'store::' || s.id || '::store.address'
     AND jsonb_typeof(scoped.value) = 'string'
    LEFT JOIN config bare
      ON bare.key = 'store.address'
     AND jsonb_typeof(bare.value) = 'string'
) c
WHERE c.line1 IS NOT NULL
  AND char_length(c.line1) <= 255
  AND c.country ~ '^[A-Z]{2}$'
  -- Control chars ([[:cntrl:]]), NEL, bidi overrides/isolates, zero-width (match NewAddress)
  AND c.line1 !~ '[[:cntrl:]]'
  AND c.line1 !~ U&'[\0085\200B-\200D\202A-\202E\2066-\2069\FEFF]'
ON CONFLICT (store_id, role) DO NOTHING;
