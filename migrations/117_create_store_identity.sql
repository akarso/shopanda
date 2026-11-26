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
-- when the scoped key exists it wins (even if empty — do not fall back to bare);
-- otherwise use bare store.address. Collapse textarea newlines to spaces before
-- NewAddress-equivalent checks. Keep in sync with postgres.StoreAddressBackfillSQL.
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
