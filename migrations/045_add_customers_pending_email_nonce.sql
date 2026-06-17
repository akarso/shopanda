-- Latest issued email-change token nonce per customer.
-- A new change request overwrites it so previously issued links are rejected;
-- it is cleared once a change is confirmed.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS pending_email_nonce TEXT NOT NULL DEFAULT '';
