CREATE TABLE tax_rates (
    id      UUID PRIMARY KEY,
    country TEXT NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    class   TEXT NOT NULL CHECK (class <> ''),
    rate    INTEGER NOT NULL CHECK (rate >= 0),
    UNIQUE (country, class)
);

CREATE INDEX idx_tax_rates_country ON tax_rates (country);
