CREATE TABLE customer_groups (
    id          UUID PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL CHECK (length(btrim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customer_groups_code ON customer_groups (code);

CREATE TABLE customer_group_members (
    customer_id UUID NOT NULL PRIMARY KEY REFERENCES customers(id) ON DELETE CASCADE,
    group_id    UUID NOT NULL REFERENCES customer_groups(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customer_group_members_group ON customer_group_members (group_id);
