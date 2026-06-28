package cms

import "context"

// ContentBlockRepository persists reusable blocks and their placements.
type ContentBlockRepository interface {
	List(ctx context.Context, offset, limit int) ([]*ContentBlock, error)
	FindByID(ctx context.Context, id string) (*ContentBlock, error)
	Create(ctx context.Context, block *ContentBlock) error
	Update(ctx context.Context, block *ContentBlock) error
	Delete(ctx context.Context, id string) error
	FindBlocksByTarget(ctx context.Context, targetType TargetType, targetKey string) ([]*ContentBlock, error)
	SaveTargetPlacements(ctx context.Context, targetType TargetType, targetKey string, blockIDs []string) error
}
