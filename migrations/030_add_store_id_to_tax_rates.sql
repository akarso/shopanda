-- Add store scoping to tax rates: each rate is now per country+class+store.
-- Empty store_id ('') represents the global/default rate.

ALTER TABLE tax_rates ADD COLUMN store_id TEXT NOT NULL DEFAULT '';

-- Drop the old unique constraint and create a new one including store_id.
ALTER TABLE tax_rates DROP CONSTRAINT tax_rates_country_class_key;
ALTER TABLE tax_rates ADD CONSTRAINT tax_rates_country_class_store_key UNIQUE (country, class, store_id);
