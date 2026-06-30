package webhook

import "context"

// Repository persists merchant webhook endpoints.
type Repository interface {
	List(ctx context.Context) ([]Endpoint, error)
	ListActive(ctx context.Context) ([]Endpoint, error)
	FindByID(ctx context.Context, id string) (*Endpoint, error)
	Create(ctx context.Context, endpoint *Endpoint) error
	Update(ctx context.Context, endpoint *Endpoint) error
	Delete(ctx context.Context, id string) error
}
