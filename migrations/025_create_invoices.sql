CREATE SEQUENCE invoice_number_seq;
CREATE SEQUENCE credit_note_number_seq;

CREATE TABLE invoices (
    id              UUID PRIMARY KEY,
    invoice_number  BIGINT NOT NULL UNIQUE DEFAULT nextval('invoice_number_seq'),
    order_id        UUID NOT NULL REFERENCES orders(id) UNIQUE,
    customer_id     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'issued'
                    CHECK (status IN ('issued')),
    currency        TEXT NOT NULL CHECK (length(currency) = 3),
    subtotal_amount BIGINT NOT NULL CHECK (subtotal_amount >= 0),
    tax_amount      BIGINT NOT NULL CHECK (tax_amount >= 0),
    total_amount    BIGINT NOT NULL CHECK (total_amount >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, order_id)
);

CREATE INDEX idx_invoices_customer ON invoices (customer_id);

CREATE TABLE invoice_items (
    invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    variant_id TEXT NOT NULL,
    sku        TEXT NOT NULL,
    name       TEXT NOT NULL,
    quantity   INT  NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    currency   TEXT NOT NULL,
    PRIMARY KEY (invoice_id, variant_id)
);

CREATE TABLE credit_notes (
    id                 UUID PRIMARY KEY,
    credit_note_number BIGINT NOT NULL UNIQUE DEFAULT nextval('credit_note_number_seq'),
    invoice_id         UUID NOT NULL,
    order_id           UUID NOT NULL,
    customer_id        TEXT NOT NULL,
    reason             TEXT NOT NULL,
    currency           TEXT NOT NULL CHECK (length(currency) = 3),
    subtotal_amount    BIGINT NOT NULL CHECK (subtotal_amount >= 0),
    tax_amount         BIGINT NOT NULL CHECK (tax_amount >= 0),
    total_amount       BIGINT NOT NULL CHECK (total_amount >= 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (invoice_id, order_id) REFERENCES invoices(id, order_id)
);

CREATE INDEX idx_credit_notes_invoice ON credit_notes (invoice_id);
CREATE INDEX idx_credit_notes_order ON credit_notes (order_id);

CREATE TABLE credit_note_items (
    credit_note_id UUID NOT NULL REFERENCES credit_notes(id) ON DELETE CASCADE,
    variant_id     TEXT NOT NULL,
    sku            TEXT NOT NULL,
    name           TEXT NOT NULL,
    quantity       INT  NOT NULL CHECK (quantity > 0),
    unit_price     BIGINT NOT NULL CHECK (unit_price >= 0),
    currency       TEXT NOT NULL,
    PRIMARY KEY (credit_note_id, variant_id)
);
