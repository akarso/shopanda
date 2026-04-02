CREATE TABLE reservations (
    id          UUID PRIMARY KEY,
    variant_id  UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    quantity    INT NOT NULL CHECK (quantity > 0),
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'confirmed')),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_reservations_variant_status ON reservations (variant_id, status);
CREATE INDEX idx_reservations_expires_at ON reservations (expires_at) WHERE status = 'active';
