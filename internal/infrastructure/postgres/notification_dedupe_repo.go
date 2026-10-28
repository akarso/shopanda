package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// notificationDedupeStaleInterval is how long a pending claim may block
// retries before TryInsert reclaims it (covers rare cleanup failures).
const notificationDedupeStaleInterval = "10 minutes"

// NotificationDedupeRepo claims one notification send per (subject_key, kind).
type NotificationDedupeRepo struct {
	db *sql.DB
}

// NewNotificationDedupeRepo returns a repo backed by db.
func NewNotificationDedupeRepo(db *sql.DB) (*NotificationDedupeRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewNotificationDedupeRepo: nil *sql.DB")
	}
	return &NotificationDedupeRepo{db: db}, nil
}

// TryInsert records a pending (subjectKey, kind) claim. inserted is true when
// the row is new or when an unfinished claim older than
// notificationDedupeStaleInterval was reclaimed. Completed claims are never
// reclaimed.
func (r *NotificationDedupeRepo) TryInsert(ctx context.Context, subjectKey, kind string) (inserted bool, err error) {
	subjectKey = strings.TrimSpace(subjectKey)
	kind = strings.TrimSpace(kind)
	if subjectKey == "" || kind == "" {
		return false, fmt.Errorf("notification_dedupe: subject_key and kind required")
	}
	// Reclaim only unfinished claims left behind when enqueue failed and
	// cleanup did not run (e.g. process crash), then claim. Completed and
	// fresh pending claims still conflict.
	const q = `
WITH reclaimed AS (
	DELETE FROM notification_dedupe
	WHERE subject_key = $1 AND kind = $2
	  AND status = 'pending'
	  AND created_at < now() - $3::interval
	RETURNING 1
)
INSERT INTO notification_dedupe (subject_key, kind, status)
VALUES ($1, $2, 'pending')
ON CONFLICT (subject_key, kind) DO NOTHING`
	res, err := r.db.ExecContext(ctx, q, subjectKey, kind, notificationDedupeStaleInterval)
	if err != nil {
		return false, fmt.Errorf("notification_dedupe insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("notification_dedupe rows: %w", err)
	}
	return n == 1, nil
}

// MarkCompleted makes a successful claim permanent so stale reclaim cannot
// allow another send.
func (r *NotificationDedupeRepo) MarkCompleted(ctx context.Context, subjectKey, kind string) error {
	subjectKey = strings.TrimSpace(subjectKey)
	kind = strings.TrimSpace(kind)
	if subjectKey == "" || kind == "" {
		return fmt.Errorf("notification_dedupe: subject_key and kind required")
	}
	const q = `
UPDATE notification_dedupe
SET status = 'completed'
WHERE subject_key = $1 AND kind = $2`
	if _, err := r.db.ExecContext(ctx, q, subjectKey, kind); err != nil {
		return fmt.Errorf("notification_dedupe mark completed: %w", err)
	}
	return nil
}

// Delete removes a claim so a later send attempt can retry after enqueue failure.
func (r *NotificationDedupeRepo) Delete(ctx context.Context, subjectKey, kind string) error {
	subjectKey = strings.TrimSpace(subjectKey)
	kind = strings.TrimSpace(kind)
	if subjectKey == "" || kind == "" {
		return fmt.Errorf("notification_dedupe: subject_key and kind required")
	}
	const q = `DELETE FROM notification_dedupe WHERE subject_key = $1 AND kind = $2`
	if _, err := r.db.ExecContext(ctx, q, subjectKey, kind); err != nil {
		return fmt.Errorf("notification_dedupe delete: %w", err)
	}
	return nil
}
