package postgres

// StoreAddressBackfillSQL is the general-address backfill from migration 117.
// Tests re-run this statement after seeding config; keep it identical to the
// INSERT in migrations/117_create_store_identity.sql.
const StoreAddressBackfillSQL = `
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
  AND c.line1 !~ '[[:cntrl:]]'
  AND c.line1 !~ U&'[\0085\200B-\200D\202A-\202E\2066-\2069\FEFF]'
ON CONFLICT (store_id, role) DO NOTHING
`
