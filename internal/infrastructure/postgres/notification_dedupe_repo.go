package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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

// TryInsert records (subjectKey, kind). inserted is true when the row is new.
func (r *NotificationDedupeRepo) TryInsert(ctx context.Context, subjectKey, kind string) (inserted bool, err error) {
	subjectKey = strings.TrimSpace(subjectKey)
	kind = strings.TrimSpace(kind)
	if subjectKey == "" || kind == "" {
		return false, fmt.Errorf("notification_dedupe: subject_key and kind required")
	}
	const q = `INSERT INTO notification_dedupe (subject_key, kind)
		VALUES ($1, $2)
		ON CONFLICT (subject_key, kind) DO NOTHING`
	res, err := r.db.ExecContext(ctx, q, subjectKey, kind)
	if err != nil {
		return false, fmt.Errorf("notification_dedupe insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("notification_dedupe rows: %w", err)
	}
	return n == 1, nil
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
