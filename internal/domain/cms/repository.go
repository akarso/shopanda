package cms

import "context"

// PageRepository persists and retrieves CMS pages.
type PageRepository interface {
	FindByID(ctx context.Context, id string) (*Page, error)
	FindBySlug(ctx context.Context, slug string) (*Page, error)
	FindActiveBySlug(ctx context.Context, slug string) (*Page, error)
	List(ctx context.Context, offset, limit int) ([]*Page, error)
	Create(ctx context.Context, p *Page) error
	Update(ctx context.Context, p *Page) error
	Delete(ctx context.Context, id string) error
}
