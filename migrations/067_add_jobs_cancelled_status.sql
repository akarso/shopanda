-- Adds the 'cancelled' terminal status (PR-1029: admin-triggered Cancel on
-- a still-pending job). Never set by the worker itself.
ALTER TABLE jobs DROP CONSTRAINT jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check
    CHECK (status IN ('pending', 'processing', 'done', 'failed', 'cancelled'));
