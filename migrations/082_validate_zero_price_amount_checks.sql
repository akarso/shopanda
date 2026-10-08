-- PR-1048 (Phase 12): validate CHECKs installed NOT VALID in 081.
-- Separate transaction so ACCESS EXCLUSIVE from 081 is gone before the
-- table scan (VALIDATE needs only SHARE UPDATE EXCLUSIVE).

ALTER TABLE price_history VALIDATE CONSTRAINT price_history_amount_check;
ALTER TABLE prices VALIDATE CONSTRAINT prices_amount_non_negative;
