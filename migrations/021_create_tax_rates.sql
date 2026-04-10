CREATE TABLE tax_rates (
    id      UUID PRIMARY KEY,
    country TEXT NOT NULL CHECK (length(country) = 2 AND country = upper(country)),
    class   TEXT NOT NULL CHECK (class <> ''),
    rate    INTEGER NOT NULL CHECK (rate >= 0),
    UNIQUE (country, class)
);

CREATE INDEX idx_tax_rates_country ON tax_rates (country);
