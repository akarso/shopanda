CREATE TABLE stock (
    variant_id  UUID PRIMARY KEY REFERENCES variants(id) ON DELETE CASCADE,
    quantity    INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
