package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/platform/id"
)

var _ domainadmin.AuditLogRepository = (*AuditLogRepo)(nil)

// AuditLogRepo implements domainadmin.AuditLogRepository.
type AuditLogRepo struct {
	db *sql.DB
}

// NewAuditLogRepo returns an AuditLogRepo backed by db.
func NewAuditLogRepo(db *sql.DB) (*AuditLogRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewAuditLogRepo: nil *sql.DB")
	}
	return &AuditLogRepo{db: db}, nil
}

func (r *AuditLogRepo) Insert(ctx context.Context, record domainadmin.AuditLogRecord) error {
	recordID := record.ID
	if recordID == "" {
		recordID = id.New()
	}
	metadata := record.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("audit_log_repo: marshal metadata: %w", err)
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	const q = `INSERT INTO admin_audit_log (
		id, created_at, admin_id, action, resource_type, resource_id,
		result, error_message, store_id, language, currency, metadata
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

	_, err = r.db.ExecContext(ctx, q,
		recordID, createdAt, record.AdminID, record.Action, record.ResourceType, record.ResourceID,
		record.Result, record.ErrorMessage, record.StoreID, record.Language, record.Currency, metaJSON,
	)
	if err != nil {
		return fmt.Errorf("audit_log_repo: insert: %w", err)
	}
	return nil
}

func (r *AuditLogRepo) List(ctx context.Context, filter domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	var (
		conds  []string
		args   []interface{}
		argNum = 1
	)

	add := func(cond string, val interface{}) {
		conds = append(conds, fmt.Sprintf(cond, argNum))
		args = append(args, val)
		argNum++
	}

	if action := strings.TrimSpace(filter.Action); action != "" {
		add("action = $%d", action)
	}
	if resourceType := strings.TrimSpace(filter.ResourceType); resourceType != "" {
		add("resource_type = $%d", resourceType)
	}
	if resourceID := strings.TrimSpace(filter.ResourceID); resourceID != "" {
		add("resource_id = $%d", resourceID)
	}
	if filter.From != nil {
		add("created_at >= $%d", filter.From.UTC())
	}
	if filter.To != nil {
		add("created_at <= $%d", filter.To.UTC())
	}

	q := `SELECT id, created_at, admin_id, action, resource_type, resource_id,
		result, error_message, store_id, language, currency, metadata
		FROM admin_audit_log`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC"
	q += fmt.Sprintf(" OFFSET $%d LIMIT $%d", argNum, argNum+1)
	args = append(args, filter.Offset, filter.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("audit_log_repo: list: %w", err)
	}
	defer rows.Close()

	out := make([]domainadmin.AuditLogRecord, 0)
	for rows.Next() {
		var rec domainadmin.AuditLogRecord
		var metaJSON []byte
		if err := rows.Scan(
			&rec.ID, &rec.CreatedAt, &rec.AdminID, &rec.Action, &rec.ResourceType, &rec.ResourceID,
			&rec.Result, &rec.ErrorMessage, &rec.StoreID, &rec.Language, &rec.Currency, &metaJSON,
		); err != nil {
			return nil, fmt.Errorf("audit_log_repo: scan: %w", err)
		}
		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &rec.Metadata); err != nil {
				return nil, fmt.Errorf("audit_log_repo: unmarshal metadata: %w", err)
			}
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit_log_repo: rows: %w", err)
	}
	return out, nil
}
