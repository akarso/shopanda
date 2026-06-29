-- Optional TOTP MFA for admin-panel users.

ALTER TABLE customers
    ADD COLUMN totp_secret_enc TEXT,
    ADD COLUMN totp_confirmed_at TIMESTAMPTZ;

CREATE TABLE admin_mfa_recovery_codes (
    id          UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_mfa_recovery_codes_customer ON admin_mfa_recovery_codes (customer_id);
