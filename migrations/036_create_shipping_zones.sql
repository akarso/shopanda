CREATE TABLE IF NOT EXISTS shipping_zones (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    countries  TEXT[] NOT NULL,
    priority   INT NOT NULL DEFAULT 0,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS shipping_rate_tiers (
    id         TEXT PRIMARY KEY,
    zone_id    TEXT NOT NULL REFERENCES shipping_zones(id) ON DELETE CASCADE,
    min_weight NUMERIC NOT NULL DEFAULT 0 CHECK (min_weight >= 0),
    max_weight NUMERIC NOT NULL DEFAULT 0 CHECK (max_weight >= 0),
    price      BIGINT NOT NULL CHECK (price >= 0),
    currency   TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    CHECK (max_weight = 0 OR max_weight >= min_weight)
);

CREATE INDEX shipping_rate_tiers_zone_idx ON shipping_rate_tiers (zone_id);
