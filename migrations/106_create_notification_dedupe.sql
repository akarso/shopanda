-- Persistent email/notification dedupe so order.paid republish (grant heal)
-- does not send a second confirmation when the queue driver is not Postgres
-- (SQS/RabbitMQ ignore job-id primary-key collisions).
-- subject_key is typically an order id; kind is e.g. order_confirmed.

CREATE TABLE IF NOT EXISTS notification_dedupe (
    subject_key TEXT        NOT NULL,
    kind        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT notification_dedupe_subject_not_empty
        CHECK (btrim(subject_key) <> ''),
    CONSTRAINT notification_dedupe_kind_not_empty
        CHECK (btrim(kind) <> ''),
    CONSTRAINT notification_dedupe_subject_kind_unique
        UNIQUE (subject_key, kind)
);
