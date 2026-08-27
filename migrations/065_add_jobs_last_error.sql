-- Records a failed job's last error message, for admin introspection
-- (PR-1028). Nullable: a job that has never failed has no error to show,
-- and existing rows have no history to backfill.
ALTER TABLE jobs ADD COLUMN last_error TEXT;
