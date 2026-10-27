-- PR-1071: bound a stored force reason. SetForced and admin HTTP reject
-- more than 500 runes. Validate on scan does not, so one long row cannot
-- fail a list. char_length counts characters and matches that rune cap
-- for valid UTF-8. NULL stays allowed for checkout and self-service binds.

ALTER TABLE product_assignments
    DROP CONSTRAINT IF EXISTS product_assignments_forced_reason_len_check;

ALTER TABLE product_assignments
    ADD CONSTRAINT product_assignments_forced_reason_len_check
    CHECK (forced_reason IS NULL OR char_length(forced_reason) <= 500);
