-- PR-1060 (Phase 12): bundle product composition rows.
-- bundle_product_id must reference a Type = bundle product (enforced in application).
-- Nesting (bundle-as-component) is rejected in application, not via CHECK here.
-- component_product_id uses ON DELETE RESTRICT so a product in use as a component
-- cannot be deleted until bundle links are removed.

CREATE TABLE IF NOT EXISTS bundle_components (
    bundle_product_id    UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    component_product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    quantity             INT  NOT NULL DEFAULT 1 CHECK (quantity >= 1 AND quantity <= 9999),
    required             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bundle_product_id, component_product_id),
    CHECK (bundle_product_id <> component_product_id)
);

CREATE INDEX IF NOT EXISTS idx_bundle_components_component
    ON bundle_components (component_product_id);
