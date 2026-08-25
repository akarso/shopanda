package groups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresRepo implements customergroup.Repository.
type PostgresRepo struct {
	db *sql.DB
}

// NewPostgresRepo creates a PostgresRepo.
func NewPostgresRepo(db *sql.DB) (*PostgresRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("b2b groups repo: nil db")
	}
	return &PostgresRepo{db: db}, nil
}

func (r *PostgresRepo) List(ctx context.Context, offset, limit int) ([]customergroup.Group, error) {
	if offset < 0 {
		return nil, fmt.Errorf("b2b groups repo: offset must be >= 0")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	const q = `SELECT id, code, name, description, created_at, updated_at
		FROM customer_groups ORDER BY name LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("b2b groups repo: list: %w", err)
	}
	defer rows.Close()
	return scanGroups(rows)
}

func (r *PostgresRepo) FindByID(ctx context.Context, id string) (*customergroup.Group, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("b2b groups repo: empty id")
	}
	const q = `SELECT id, code, name, description, created_at, updated_at
		FROM customer_groups WHERE id = $1`
	g, err := scanGroup(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("b2b groups repo: find by id: %w", err)
	}
	return g, nil
}

func (r *PostgresRepo) FindByCode(ctx context.Context, code string) (*customergroup.Group, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, fmt.Errorf("b2b groups repo: empty code")
	}
	const q = `SELECT id, code, name, description, created_at, updated_at
		FROM customer_groups WHERE code = $1`
	g, err := scanGroup(r.db.QueryRowContext(ctx, q, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("b2b groups repo: find by code: %w", err)
	}
	return g, nil
}

func (r *PostgresRepo) Save(ctx context.Context, group *customergroup.Group) error {
	if group == nil {
		return fmt.Errorf("b2b groups repo: group must not be nil")
	}
	const upsert = `INSERT INTO customer_groups (id, code, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at`
	_, err := r.db.ExecContext(ctx, upsert,
		group.ID, group.Code, group.Name, group.Description, group.CreatedAt, group.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("b2b groups repo: code already exists")
		}
		return fmt.Errorf("b2b groups repo: save: %w", err)
	}
	return nil
}

func (r *PostgresRepo) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("b2b groups repo: empty id")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM customer_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("b2b groups repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("b2b groups repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("b2b groups repo: group not found")
	}
	return nil
}

func (r *PostgresRepo) AssignCustomer(ctx context.Context, customerID, groupID string) error {
	customerID = strings.TrimSpace(customerID)
	groupID = strings.TrimSpace(groupID)
	if customerID == "" || groupID == "" {
		return fmt.Errorf("b2b groups repo: customer id and group id required")
	}
	const q = `INSERT INTO customer_group_members (customer_id, group_id, created_at)
		VALUES ($1, $2, now())
		ON CONFLICT (customer_id) DO UPDATE SET group_id = EXCLUDED.group_id`
	_, err := r.db.ExecContext(ctx, q, customerID, groupID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return fmt.Errorf("b2b groups repo: customer or group not found")
		}
		return fmt.Errorf("b2b groups repo: assign customer: %w", err)
	}
	return nil
}

func (r *PostgresRepo) RemoveCustomer(ctx context.Context, customerID string) error {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return fmt.Errorf("b2b groups repo: empty customer id")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM customer_group_members WHERE customer_id = $1`, customerID)
	if err != nil {
		return fmt.Errorf("b2b groups repo: remove customer: %w", err)
	}
	return nil
}

func (r *PostgresRepo) FindGroupByCustomerID(ctx context.Context, customerID string) (*customergroup.Group, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, fmt.Errorf("b2b groups repo: empty customer id")
	}
	const q = `SELECT g.id, g.code, g.name, g.description, g.created_at, g.updated_at
		FROM customer_groups g
		INNER JOIN customer_group_members m ON m.group_id = g.id
		WHERE m.customer_id = $1`
	g, err := scanGroup(r.db.QueryRowContext(ctx, q, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("b2b groups repo: find by customer: %w", err)
	}
	return g, nil
}

type groupScanner interface {
	Scan(dest ...interface{}) error
}

func scanGroup(s groupScanner) (*customergroup.Group, error) {
	var g customergroup.Group
	if err := s.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

func scanGroups(rows *sql.Rows) ([]customergroup.Group, error) {
	var groups []customergroup.Group
	for rows.Next() {
		var g customergroup.Group
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("b2b groups repo: scan: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("b2b groups repo: rows: %w", err)
	}
	return groups, nil
}

// Compile-time interface check.
var _ customergroup.Repository = (*PostgresRepo)(nil)
