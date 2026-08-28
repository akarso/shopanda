-- Adds the 'cancelled' terminal status (PR-1029: admin-triggered Cancel on
-- a still-pending job). Never set by the worker itself.
--
-- jobs is the busiest table in the system (worker polls/updates it
-- continuously), so this avoids the plain DROP+ADD CHECK pattern: a bare
-- ADD CONSTRAINT ... CHECK holds ACCESS EXCLUSIVE for the full validation
-- scan of every existing row, blocking the worker's reads/writes for that
-- whole duration. Instead: add the new constraint NOT VALID (brief
-- exclusive lock, no scan — new/updated rows are checked immediately),
-- drop the old one, rename into place, then VALIDATE CONSTRAINT separately
-- (SHARE UPDATE EXCLUSIVE — scans existing rows without blocking
-- concurrent reads/writes). Same locking concern as migrations
-- 050/066, different mechanism since a CHECK constraint (unlike an index)
-- has no CONCURRENTLY variant.
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check_new
    CHECK (status IN ('pending', 'processing', 'done', 'failed', 'cancelled')) NOT VALID;
ALTER TABLE jobs DROP CONSTRAINT jobs_status_check;
ALTER TABLE jobs RENAME CONSTRAINT jobs_status_check_new TO jobs_status_check;
ALTER TABLE jobs VALIDATE CONSTRAINT jobs_status_check;
