package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

var _ cms.MenuRepository = (*MenuRepo)(nil)

// MenuRepo implements cms.MenuRepository using PostgreSQL.
type MenuRepo struct {
	db *sql.DB
}

// NewMenuRepo returns a new MenuRepo backed by db.
func NewMenuRepo(db *sql.DB) (*MenuRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewMenuRepo: nil *sql.DB")
	}
	return &MenuRepo{db: db}, nil
}

const menuColumns = `id, code, title, is_active, created_at, updated_at`

const menuItemColumns = `id, menu_id, parent_id, label, link_type, link_target, position, is_active, created_at, updated_at`

func hydrateMenu(scan func(dest ...interface{}) error) (*cms.Menu, error) {
	var id, code, title string
	var isActive bool
	var createdAt, updatedAt time.Time
	if err := scan(&id, &code, &title, &isActive, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return cms.NewMenuFromDB(id, code, title, isActive, createdAt, updatedAt), nil
}

func hydrateMenuItem(scan func(dest ...interface{}) error) (*cms.MenuItem, error) {
	var id, menuID, label, linkType, linkTarget string
	var parentID sql.NullString
	var position int
	var isActive bool
	var createdAt, updatedAt time.Time
	if err := scan(&id, &menuID, &parentID, &label, &linkType, &linkTarget, &position, &isActive, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	parent := ""
	if parentID.Valid {
		parent = parentID.String
	}
	return cms.NewMenuItemFromDB(
		id, menuID, parent, label,
		cms.LinkType(linkType),
		linkTarget,
		position,
		isActive,
		createdAt,
		updatedAt,
	), nil
}

// List returns all menus ordered by code.
func (r *MenuRepo) List(ctx context.Context) ([]*cms.Menu, error) {
	q := `SELECT ` + menuColumns + ` FROM menus ORDER BY code`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("menu_repo: list: %w", err)
	}
	defer rows.Close()

	var menus []*cms.Menu
	for rows.Next() {
		menu, err := hydrateMenu(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("menu_repo: list scan: %w", err)
		}
		menus = append(menus, menu)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("menu_repo: list rows: %w", err)
	}
	return menus, nil
}

// FindByID returns a menu with items by ID.
func (r *MenuRepo) FindByID(ctx context.Context, id string) (*cms.MenuWithItems, error) {
	if id == "" {
		return nil, fmt.Errorf("menu_repo: find by id: empty id")
	}
	q := `SELECT ` + menuColumns + ` FROM menus WHERE id = $1`
	menu, err := hydrateMenu(r.db.QueryRowContext(ctx, q, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("menu_repo: find by id: %w", err)
	}
	items, err := r.listItems(ctx, menu.ID())
	if err != nil {
		return nil, err
	}
	return &cms.MenuWithItems{Menu: menu, Items: items}, nil
}

// FindByCode returns a menu with items by code.
func (r *MenuRepo) FindByCode(ctx context.Context, code string) (*cms.MenuWithItems, error) {
	if code == "" {
		return nil, fmt.Errorf("menu_repo: find by code: empty code")
	}
	q := `SELECT ` + menuColumns + ` FROM menus WHERE code = $1`
	menu, err := hydrateMenu(r.db.QueryRowContext(ctx, q, code).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("menu_repo: find by code: %w", err)
	}
	items, err := r.listItems(ctx, menu.ID())
	if err != nil {
		return nil, err
	}
	return &cms.MenuWithItems{Menu: menu, Items: items}, nil
}

func (r *MenuRepo) listItems(ctx context.Context, menuID string) ([]*cms.MenuItem, error) {
	q := `SELECT ` + menuItemColumns + ` FROM menu_items WHERE menu_id = $1 ORDER BY position, label`
	rows, err := r.db.QueryContext(ctx, q, menuID)
	if err != nil {
		return nil, fmt.Errorf("menu_repo: list items: %w", err)
	}
	defer rows.Close()

	var items []*cms.MenuItem
	for rows.Next() {
		item, err := hydrateMenuItem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("menu_repo: list items scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("menu_repo: list items rows: %w", err)
	}
	return items, nil
}

// Save updates menu metadata and replaces all items atomically.
func (r *MenuRepo) Save(ctx context.Context, data *cms.MenuWithItems) error {
	if data == nil || data.Menu == nil {
		return fmt.Errorf("menu_repo: save: nil menu")
	}
	if err := cms.ValidateMenuItems(data.Items); err != nil {
		return apperror.Validation(err.Error())
	}
	menu := data.Menu
	for _, item := range data.Items {
		if item.MenuID() != menu.ID() {
			return apperror.Validation(fmt.Sprintf(
				"menu items: item %q belongs to menu %q, expected %q",
				item.ID(), item.MenuID(), menu.ID(),
			))
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("menu_repo: save begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE menus
		SET title = $2, is_active = $3, updated_at = now()
		WHERE id = $1`,
		menu.ID(), menu.Title(), menu.IsActive(),
	)
	if err != nil {
		return fmt.Errorf("menu_repo: save update menu: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("menu_repo: save rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("menu not found")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM menu_items WHERE menu_id = $1`, menu.ID()); err != nil {
		return fmt.Errorf("menu_repo: save delete items: %w", err)
	}

	inserted := make(map[string]struct{}, len(data.Items))
	for len(inserted) < len(data.Items) {
		progress := false
		for _, item := range data.Items {
			if _, ok := inserted[item.ID()]; ok {
				continue
			}
			parentID := item.ParentID()
			if parentID != "" {
				if _, ok := inserted[parentID]; !ok {
					continue
				}
			}
			var parentArg interface{}
			if parentID != "" {
				parentArg = parentID
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO menu_items (
					id, menu_id, parent_id, label, link_type, link_target, position, is_active, created_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())`,
				item.ID(),
				menu.ID(),
				parentArg,
				item.Label(),
				string(item.LinkType()),
				item.LinkTarget(),
				item.Position(),
				item.IsActive(),
			)
			if err != nil {
				return fmt.Errorf("menu_repo: save insert item %q: %w", item.ID(), err)
			}
			inserted[item.ID()] = struct{}{}
			progress = true
		}
		if !progress {
			return apperror.Validation("menu items: unresolved parent ordering")
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("menu_repo: save commit: %w", err)
	}
	return nil
}
