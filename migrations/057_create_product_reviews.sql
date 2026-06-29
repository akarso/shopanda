-- Moderated product reviews submitted by signed-in customers.

CREATE TABLE product_reviews (
    id          UUID PRIMARY KEY,
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    rating      INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
    title       TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (product_id, customer_id)
);

CREATE INDEX idx_product_reviews_product_status ON product_reviews (product_id, status, created_at DESC);
CREATE INDEX idx_product_reviews_status_created ON product_reviews (status, created_at DESC);
