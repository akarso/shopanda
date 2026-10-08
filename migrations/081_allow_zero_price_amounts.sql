-- PR-1048 (Phase 12): domain constructors accept amount 0 (free catalog
-- prices). Align storage CHECKs so Omnibus history and the prices table
-- can persist a free price. price_history previously required amount > 0;
-- prices had no amount CHECK at all.
--
-- Each migration file runs in one transaction (applyMigrationContent).
-- ADD/DROP/RENAME take ACCESS EXCLUSIVE and hold it until commit, so
-- VALIDATE must not share this file — see 082 (SHARE UPDATE EXCLUSIVE
-- scan after these stronger locks are released). Same intent as
-- migrations/067, corrected for the per-file transaction boundary.

ALTER TABLE price_history ADD CONSTRAINT price_history_amount_check_new
    CHECK (amount >= 0) NOT VALID;
ALTER TABLE price_history DROP CONSTRAINT price_history_amount_check;
ALTER TABLE price_history RENAME CONSTRAINT price_history_amount_check_new TO price_history_amount_check;

ALTER TABLE prices ADD CONSTRAINT prices_amount_non_negative
    CHECK (amount >= 0) NOT VALID;
