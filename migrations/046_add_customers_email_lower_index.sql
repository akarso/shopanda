-- Case-insensitive email lookups (auth register/login/reset) use LOWER(email) = $1.
CREATE INDEX IF NOT EXISTS idx_customers_email_lower ON customers (LOWER(email));
