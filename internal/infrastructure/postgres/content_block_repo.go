package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ cms.ContentBlockRepository = (*ContentBlockRepo)(nil)

// ContentBlockRepo implements cms.ContentBlockRepository using PostgreSQL.
type ContentBlockRepo struct {
	db *sql.DB
}

// NewContentBlockRepo returns a new ContentBlockRepo backed by db.
func NewContentBlockRepo(db *sql.DB) (*ContentBlockRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewContentBlockRepo: nil *sql.DB")
	}
	return &ContentBlockRepo{db: db}, nil
}

const contentBlockColumns = `id, title, block_type, config, is_active, created_at, updated_at`
const contentBlockColumnsAliased = `b.id, b.title, b.block_type, b.config, b.is_active, b.created_at, b.updated_at`

func hydrateContentBlock(scan func(dest ...interface{}) error) (*cms.ContentBlock, error) {
	var blockID, title, blockType string
	var configJSON []byte
	var isActive bool
	var createdAt, updatedAt time.Time
	if err := scan(&blockID, &title, &blockType, &configJSON, &isActive, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	config := map[string]interface{}{}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &config); err != nil {
			return nil, fmt.Errorf("content_block_repo: decode config: %w", err)
		}
	}
	return cms.NewContentBlockFromDB(blockID, title, cms.BlockType(blockType), config, isActive, createdAt, updatedAt), nil
}

// List returns content blocks ordered by title.
func (r *ContentBlockRepo) List(ctx context.Context, offset, limit int) ([]*cms.ContentBlock, error) {
	if offset < 0 {
		return nil, fmt.Errorf("content_block_repo: list: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("content_block_repo: list: limit must be positive")
	}
	q := `SELECT ` + contentBlockColumns + ` FROM content_blocks ORDER BY title LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("content_block_repo: list: %w", err)
	}
	defer rows.Close()

	var blocks []*cms.ContentBlock
	for rows.Next() {
		block, err := hydrateContentBlock(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("content_block_repo: list scan: %w", err)
		}
		blocks = append(blocks, block)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("content_block_repo: list rows: %w", err)
	}
	return blocks, nil
}

// FindByID returns a block by ID.
func (r *ContentBlockRepo) FindByID(ctx context.Context, blockID string) (*cms.ContentBlock, error) {
	if blockID == "" {
		return nil, fmt.Errorf("content_block_repo: find by id: empty id")
	}
	q := `SELECT ` + contentBlockColumns + ` FROM content_blocks WHERE id = $1`
	block, err := hydrateContentBlock(r.db.QueryRowContext(ctx, q, blockID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("content_block_repo: find by id: %w", err)
	}
	return block, nil
}

// Create persists a new content block.
func (r *ContentBlockRepo) Create(ctx context.Context, block *cms.ContentBlock) error {
	if block == nil {
		return fmt.Errorf("content_block_repo: create: nil block")
	}
	configJSON, err := json.Marshal(block.Config())
	if err != nil {
		return fmt.Errorf("content_block_repo: create encode config: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO content_blocks (id, title, block_type, config, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		block.ID(), block.Title(), string(block.BlockType()), configJSON,
		block.IsActive(), block.CreatedAt(), block.UpdatedAt(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperror.Conflict("content block already exists")
		}
		return fmt.Errorf("content_block_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing content block.
func (r *ContentBlockRepo) Update(ctx context.Context, block *cms.ContentBlock) error {
	if block == nil {
		return fmt.Errorf("content_block_repo: update: nil block")
	}
	configJSON, err := json.Marshal(block.Config())
	if err != nil {
		return fmt.Errorf("content_block_repo: update encode config: %w", err)
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE content_blocks
		SET title = $2, block_type = $3, config = $4, is_active = $5, updated_at = now()
		WHERE id = $1`,
		block.ID(), block.Title(), string(block.BlockType()), configJSON, block.IsActive(),
	)
	if err != nil {
		return fmt.Errorf("content_block_repo: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("content_block_repo: update rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("content block not found")
	}
	return nil
}

// Delete removes a content block by ID.
func (r *ContentBlockRepo) Delete(ctx context.Context, blockID string) error {
	if blockID == "" {
		return fmt.Errorf("content_block_repo: delete: empty id")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM content_blocks WHERE id = $1`, blockID)
	if err != nil {
		return fmt.Errorf("content_block_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("content_block_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("content block not found")
	}
	return nil
}

// FindBlocksByTarget returns placed blocks for admin views, including inactive blocks.
func (r *ContentBlockRepo) FindBlocksByTarget(ctx context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
	return r.findBlocksByTarget(ctx, targetType, targetKey, false)
}

// FindActiveBlocksByTarget returns active placed blocks for storefront and public APIs.
func (r *ContentBlockRepo) FindActiveBlocksByTarget(ctx context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
	return r.findBlocksByTarget(ctx, targetType, targetKey, true)
}

func (r *ContentBlockRepo) findBlocksByTarget(ctx context.Context, targetType cms.TargetType, targetKey string, activeOnly bool) ([]*cms.ContentBlock, error) {
	if !cms.ValidTargetType(targetType) {
		return nil, fmt.Errorf("content_block_repo: find by target: invalid target type")
	}
	targetKey = cms.NormalizeTargetKey(targetKey)
	if targetKey == "" {
		return nil, fmt.Errorf("content_block_repo: find by target: empty target key")
	}
	q := `
		SELECT ` + contentBlockColumnsAliased + `
		FROM content_block_placements p
		JOIN content_blocks b ON b.id = p.block_id
		WHERE p.target_type = $1
		  AND p.target_key = $2
		  AND p.is_active = true`
	if activeOnly {
		q += `
		  AND b.is_active = true`
	}
	q += `
		ORDER BY p.position, b.title`
	rows, err := r.db.QueryContext(ctx, q, string(targetType), targetKey)
	if err != nil {
		return nil, fmt.Errorf("content_block_repo: find by target: %w", err)
	}
	defer rows.Close()

	var blocks []*cms.ContentBlock
	for rows.Next() {
		block, err := hydrateContentBlock(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("content_block_repo: find by target scan: %w", err)
		}
		blocks = append(blocks, block)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("content_block_repo: find by target rows: %w", err)
	}
	return blocks, nil
}

// SaveTargetPlacements replaces all placements for a target with the given block IDs.
func (r *ContentBlockRepo) SaveTargetPlacements(ctx context.Context, targetType cms.TargetType, targetKey string, blockIDs []string) error {
	if !cms.ValidTargetType(targetType) {
		return apperror.Validation("invalid target type")
	}
	if targetKey == "" {
		return apperror.Validation("target key is required")
	}
	targetKey = cms.NormalizeTargetKey(targetKey)
	if targetKey == "" {
		return apperror.Validation("target key is required")
	}
	if targetType == cms.TargetTypeLayout && !cms.ValidLayoutTarget(targetKey) {
		return apperror.Validation("invalid layout target")
	}
	seen := make(map[string]struct{}, len(blockIDs))
	for _, blockID := range blockIDs {
		if blockID == "" {
			return apperror.Validation("block id is required")
		}
		if !id.IsValid(blockID) {
			return apperror.Validation("invalid block id")
		}
		if _, ok := seen[blockID]; ok {
			return apperror.Validation(fmt.Sprintf("duplicate block id %q in placement list", blockID))
		}
		seen[blockID] = struct{}{}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("content_block_repo: save placements begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	lockKey := string(targetType) + ":" + targetKey
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return fmt.Errorf("content_block_repo: save placements lock: %w", err)
	}

	for _, blockID := range blockIDs {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content_blocks WHERE id = $1)`, blockID).Scan(&exists); err != nil {
			return fmt.Errorf("content_block_repo: save placements check block: %w", err)
		}
		if !exists {
			return apperror.Validation(fmt.Sprintf("content block %q not found", blockID))
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content_block_placements
		WHERE target_type = $1 AND target_key = $2`,
		string(targetType), targetKey,
	); err != nil {
		return fmt.Errorf("content_block_repo: save placements delete: %w", err)
	}

	for position, blockID := range blockIDs {
		placementID := id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO content_block_placements (
				id, block_id, target_type, target_key, position, is_active, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, true, now(), now())`,
			placementID, blockID, string(targetType), targetKey, position,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperror.Conflict("content block placements conflict for target")
			}
			return fmt.Errorf("content_block_repo: save placements insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("content_block_repo: save placements commit: %w", err)
	}
	return nil
}
