-- Cookie consent preferences per customer.
CREATE TABLE consents (
    customer_id TEXT NOT NULL PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    necessary   BOOLEAN NOT NULL DEFAULT TRUE,
    analytics   BOOLEAN NOT NULL DEFAULT FALSE,
    marketing   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
