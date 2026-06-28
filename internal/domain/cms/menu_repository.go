package cms

import "context"

// MenuRepository persists and retrieves navigation menus.
type MenuRepository interface {
	List(ctx context.Context) ([]*Menu, error)
	FindByID(ctx context.Context, id string) (*MenuWithItems, error)
	FindByCode(ctx context.Context, code string) (*MenuWithItems, error)
	Save(ctx context.Context, menu *MenuWithItems) error
}
