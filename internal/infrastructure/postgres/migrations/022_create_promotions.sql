CREATE TABLE promotions (
    id           UUID PRIMARY KEY,
    name         TEXT    NOT NULL CHECK (name <> ''),
    type         TEXT    NOT NULL CHECK (type IN ('catalog', 'cart')),
    priority     INTEGER NOT NULL DEFAULT 0,
    active       BOOLEAN NOT NULL DEFAULT true,
    start_at     TIMESTAMPTZ,
    end_at       TIMESTAMPTZ,
    conditions   JSONB   NOT NULL DEFAULT '[]',
    actions      JSONB   NOT NULL DEFAULT '[]',
    coupon_bound BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_promotions_type_active ON promotions (type, active);

CREATE TABLE coupons (
    id           UUID PRIMARY KEY,
    code         TEXT NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9][A-Z0-9\-]{1,48}[A-Z0-9]$'),
    promotion_id UUID NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
    usage_limit  INTEGER NOT NULL DEFAULT 0 CHECK (usage_limit >= 0),
    usage_count  INTEGER NOT NULL DEFAULT 0 CHECK (usage_count >= 0),
    active       BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_coupons_promotion_id ON coupons (promotion_id);
CREATE UNIQUE INDEX idx_coupons_code ON coupons (code);
