-- Distinguish unfinished claims from successful sends so stale reclaim
-- cannot delete a completed claim and allow a second confirmation email
-- on order.paid republish (SQS/RabbitMQ ignore job-id collisions).
-- Existing rows (if any) are successful claims → completed.

ALTER TABLE notification_dedupe
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'completed';

ALTER TABLE notification_dedupe
    DROP CONSTRAINT IF EXISTS notification_dedupe_status_check;

ALTER TABLE notification_dedupe
    ADD CONSTRAINT notification_dedupe_status_check
    CHECK (status IN ('pending', 'completed'));
