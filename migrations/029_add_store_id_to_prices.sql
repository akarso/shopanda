-- Add store scoping to prices: each price is now per variant+currency+store.
-- Empty store_id ('') represents the global/default price.
-- NOTE: storefront search projects store-aware prices from this table; if that
-- path becomes hot, consider a supporting index for the projection order such
-- as one including store_id, amount, and created_at.

ALTER TABLE prices ADD COLUMN store_id TEXT NOT NULL DEFAULT '';

-- Drop the old unique constraint and create a new one including store_id.
ALTER TABLE prices DROP CONSTRAINT prices_variant_id_currency_key;
ALTER TABLE prices ADD CONSTRAINT prices_variant_store_currency_key UNIQUE (variant_id, currency, store_id);
