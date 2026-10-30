-- PR-1076: copy every non-zero standard tax rate into class 'digital'
-- so fail-closed downloadable VAT has a migrate-path row without requiring seed.
-- Idempotent: ON CONFLICT DO NOTHING leaves operator-tuned digital rows alone.
-- Zero-bps standard rows are skipped — intentional 0% uses class 'zero', not
-- a silent digital Rate==0 row (calculator refuses digital Rate==0).

INSERT INTO tax_rates (id, country, class, store_id, rate)
SELECT gen_random_uuid(), country, 'digital', store_id, rate
FROM tax_rates
WHERE class = 'standard'
  AND rate > 0
ON CONFLICT (country, class, store_id) DO NOTHING;
