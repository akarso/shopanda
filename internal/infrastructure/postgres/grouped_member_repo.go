package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that GroupedMemberRepo implements catalog.GroupedMemberRepository.
var _ catalog.GroupedMemberRepository = (*GroupedMemberRepo)(nil)

// GroupedMemberRepo implements catalog.GroupedMemberRepository using PostgreSQL.
// Membership rows are display links only; they do not affect Purchasable or visibility.
//
// WithTx is adapter-only. The domain GroupedMemberRepository port does not declare it.
type GroupedMemberRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// groupedQueryer is the subset of *sql.DB and *sql.Tx used by this repo.
type groupedQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewGroupedMemberRepo returns a new GroupedMemberRepo backed by db.
func NewGroupedMemberRepo(db *sql.DB) (*GroupedMemberRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewGroupedMemberRepo: nil *sql.DB")
	}
	return &GroupedMemberRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
// This method is postgres-only; callers type-assert the concrete adapter.
func (r *GroupedMemberRepo) WithTx(tx *sql.Tx) catalog.GroupedMemberRepository {
	return &GroupedMemberRepo{db: r.db, tx: tx}
}

func (r *GroupedMemberRepo) queryer() groupedQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapGroupedMemberExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("product not found")
	case "23514":
		return apperror.Validation("grouped member constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// ListByGroupID returns members for groupProductID ordered by created_at,
// then member_product_id. The slice is non-nil when the query succeeds.
func (r *GroupedMemberRepo) ListByGroupID(ctx context.Context, groupProductID string) ([]catalog.GroupedMember, error) {
	if groupProductID == "" {
		return nil, fmt.Errorf("grouped_member_repo: list: empty group product id")
	}
	const q = `SELECT group_product_id, member_product_id, created_at
		FROM grouped_members
		WHERE group_product_id = $1
		ORDER BY created_at ASC, member_product_id ASC`

	rows, err := r.queryer().QueryContext(ctx, q, groupProductID)
	if err != nil {
		if mapped := mapGroupedMemberExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("grouped_member_repo: list: %w", err)
	}
	defer rows.Close()

	out := []catalog.GroupedMember{}
	for rows.Next() {
		var m catalog.GroupedMember
		if err := rows.Scan(
			&m.GroupProductID,
			&m.MemberProductID,
			&m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("grouped_member_repo: list scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("grouped_member_repo: list rows: %w", err)
	}
	return out, nil
}

// CountByGroupID returns how many member rows belong to the group product.
func (r *GroupedMemberRepo) CountByGroupID(ctx context.Context, groupProductID string) (int, error) {
	if groupProductID == "" {
		return 0, fmt.Errorf("grouped_member_repo: count by group: empty group product id")
	}
	const q = `SELECT COUNT(*) FROM grouped_members WHERE group_product_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, groupProductID).Scan(&n); err != nil {
		if mapped := mapGroupedMemberExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("grouped_member_repo: count by group: %w", err)
	}
	return n, nil
}

// CountByMemberID returns how many groups reference the product as a member.
func (r *GroupedMemberRepo) CountByMemberID(ctx context.Context, memberProductID string) (int, error) {
	if memberProductID == "" {
		return 0, fmt.Errorf("grouped_member_repo: count by member: empty member product id")
	}
	const q = `SELECT COUNT(*) FROM grouped_members WHERE member_product_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, memberProductID).Scan(&n); err != nil {
		if mapped := mapGroupedMemberExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("grouped_member_repo: count by member: %w", err)
	}
	return n, nil
}

// Insert adds a membership row. ON CONFLICT DO NOTHING makes a duplicate a no-op.
// Field validation only. Composition rules stay in GroupedService.Add.
// Product types are not stored or checked in this table.
func (r *GroupedMemberRepo) Insert(ctx context.Context, m *catalog.GroupedMember) error {
	if m == nil {
		return fmt.Errorf("grouped_member_repo: insert: member must not be nil")
	}
	if err := m.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO grouped_members (group_product_id, member_product_id)
		VALUES ($1, $2)
		ON CONFLICT (group_product_id, member_product_id) DO NOTHING`

	_, err := r.queryer().ExecContext(ctx, q, m.GroupProductID, m.MemberProductID)
	if err != nil {
		if mapped := mapGroupedMemberExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("grouped_member_repo: insert: %w", err)
	}
	return nil
}

// Remove deletes one group-member link.
func (r *GroupedMemberRepo) Remove(ctx context.Context, groupProductID, memberProductID string) error {
	if groupProductID == "" {
		return fmt.Errorf("grouped_member_repo: remove: empty group product id")
	}
	if memberProductID == "" {
		return fmt.Errorf("grouped_member_repo: remove: empty member product id")
	}
	const q = `DELETE FROM grouped_members
		WHERE group_product_id = $1 AND member_product_id = $2`

	res, err := r.queryer().ExecContext(ctx, q, groupProductID, memberProductID)
	if err != nil {
		if mapped := mapGroupedMemberExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("grouped_member_repo: remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("grouped_member_repo: remove rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("grouped member not found")
	}
	return nil
}
