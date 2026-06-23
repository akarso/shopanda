CREATE TABLE admin_audit_log (
    id            UUID PRIMARY KEY,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    admin_id      TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    store_id      TEXT NOT NULL DEFAULT '',
    language      TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL DEFAULT '',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX idx_admin_audit_log_created_at ON admin_audit_log (created_at DESC);
CREATE INDEX idx_admin_audit_log_action ON admin_audit_log (action);
CREATE INDEX idx_admin_audit_log_resource ON admin_audit_log (resource_type, resource_id);
