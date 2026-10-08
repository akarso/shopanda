-- PR-1048 (Phase 12): domain constructors accept amount 0 (free catalog
-- prices). Align storage CHECKs so Omnibus history and the prices table
-- can persist a free price. price_history previously required amount > 0;
-- prices had no amount CHECK at all.
--
-- NOT VALID + VALIDATE avoids a long ACCESS EXCLUSIVE validation scan
-- (same pattern as migrations/067).

ALTER TABLE price_history ADD CONSTRAINT price_history_amount_check_new
    CHECK (amount >= 0) NOT VALID;
ALTER TABLE price_history DROP CONSTRAINT price_history_amount_check;
ALTER TABLE price_history RENAME CONSTRAINT price_history_amount_check_new TO price_history_amount_check;
ALTER TABLE price_history VALIDATE CONSTRAINT price_history_amount_check;

ALTER TABLE prices ADD CONSTRAINT prices_amount_non_negative
    CHECK (amount >= 0) NOT VALID;
ALTER TABLE prices VALIDATE CONSTRAINT prices_amount_non_negative;
