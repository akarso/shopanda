package postgres

// StoreAddressBackfillSQL is the general-address backfill from migration 117.
// Tests re-run this statement after seeding config; keep it identical to the
// DO $backfill$ … $backfill$; block in migrations/117_create_store_identity.sql.
const StoreAddressBackfillSQL = `
DO $backfill$
DECLARE
    rec record;
    raw_line text;
    line1 text;
    country text;
BEGIN
    FOR rec IN
        SELECT
            s.id AS store_id,
            s.country AS store_country,
            (scoped.key IS NOT NULL) AS scoped_exists,
            scoped.value #>> '{}' AS scoped_raw,
            bare.value #>> '{}' AS bare_raw
        FROM stores s
        LEFT JOIN config scoped
          ON scoped.key = 'store::' || s.id || '::store.address'
         AND jsonb_typeof(scoped.value) = 'string'
        LEFT JOIN config bare
          ON bare.key = 'store.address'
         AND jsonb_typeof(bare.value) = 'string'
    LOOP
        IF rec.scoped_exists THEN
            raw_line := rec.scoped_raw;
        ELSE
            raw_line := rec.bare_raw;
        END IF;

        IF raw_line IS NULL THEN
            CONTINUE;
        END IF;

        -- TAB, LF, CR, NEL, LS, PS → space (textarea / line-break soft normalize)
        line1 := btrim(regexp_replace(raw_line, U&'[\0009\000A\000D\0085\2028\2029]+', ' ', 'g'));
        IF line1 = '' THEN
            CONTINUE;
        END IF;

        country := CASE
            WHEN upper(btrim(rec.store_country)) = 'UK' THEN 'GB'
            ELSE upper(btrim(rec.store_country))
        END;

        IF char_length(line1) > 255
           OR country !~ '^[A-Z]{2}$'
           OR line1 ~ '[[:cntrl:]]'
           OR line1 ~ U&'[\200B-\200D\202A-\202E\2066-\2069\FEFF]' THEN
            RAISE NOTICE 'store identity backfill skipped store_id=%', rec.store_id;
            CONTINUE;
        END IF;

        INSERT INTO store_addresses (store_id, role, line1, country)
        VALUES (rec.store_id, 'general', line1, country)
        ON CONFLICT (store_id, role) DO NOTHING;
    END LOOP;
END
$backfill$;
`
