package rewrite

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Subscriber registers URL rewrites when products or categories are
// created or updated. It listens to catalog domain events and
// saves the entity slug as a URL rewrite.
type Subscriber struct {
	rewrites routing.RewriteRepository
	log      logger.Logger
}

// NewSubscriber creates a Subscriber.
func NewSubscriber(rewrites routing.RewriteRepository, log logger.Logger) *Subscriber {
	if rewrites == nil {
		panic("rewrite.NewSubscriber: rewrites repository must not be nil")
	}
	if log == nil {
		panic("rewrite.NewSubscriber: logger must not be nil")
	}
	return &Subscriber{rewrites: rewrites, log: log}
}

// Register wires all event handlers on the given bus.
func (s *Subscriber) Register(bus *event.Bus) {
	bus.On(catalog.EventProductCreated, s.HandleProductCreated)
	bus.On(catalog.EventProductUpdated, s.HandleProductUpdated)
	bus.On(catalog.EventCategoryCreated, s.HandleCategoryCreated)
	bus.On(catalog.EventCategoryUpdated, s.HandleCategoryUpdated)
	bus.On(cms.EventPageCreated, s.HandlePageCreated)
	bus.On(cms.EventPageUpdated, s.HandlePageUpdated)
	bus.On(cms.EventPageDeleted, s.HandlePageDeleted)
}

// HandleProductCreated saves a URL rewrite for a newly created product.
func (s *Subscriber) HandleProductCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductCreatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "product", data.ProductID)
}

// HandleProductUpdated saves a URL rewrite for an updated product.
func (s *Subscriber) HandleProductUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductUpdatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "product", data.ProductID)
}

// HandleCategoryCreated saves a URL rewrite for a newly created category.
func (s *Subscriber) HandleCategoryCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryCreatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "category", data.CategoryID)
}

// HandleCategoryUpdated saves a URL rewrite for an updated category.
func (s *Subscriber) HandleCategoryUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryUpdatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "category", data.CategoryID)
}

// HandlePageCreated saves a URL rewrite for a newly created page.
func (s *Subscriber) HandlePageCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageCreatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "page", data.PageID)
}

// HandlePageUpdated saves a URL rewrite for an updated page.
func (s *Subscriber) HandlePageUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageUpdatedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	if !data.Active {
		return s.removeRewrite(ctx, "/"+data.Slug)
	}
	return s.saveRewrite(ctx, "/"+data.Slug, "page", data.PageID)
}

// HandlePageDeleted removes the URL rewrite for a deleted page.
func (s *Subscriber) HandlePageDeleted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageDeletedData)
	if !ok {
		return fmt.Errorf("rewrite: unexpected event data type %T", evt.Data)
	}
	return s.removeRewrite(ctx, "/"+data.Slug)
}

func (s *Subscriber) saveRewrite(ctx context.Context, path, typ, entityID string) error {
	existing, err := s.rewrites.FindByPath(ctx, path)
	if err != nil {
		return fmt.Errorf("rewrite: lookup: %w", err)
	}
	if existing != nil && (existing.Type() != typ || existing.EntityID() != entityID) {
		return fmt.Errorf("rewrite: path %q already claimed by %s/%s", path, existing.Type(), existing.EntityID())
	}

	rw, err := routing.NewURLRewrite(path, typ, entityID)
	if err != nil {
		return fmt.Errorf("rewrite: create: %w", err)
	}
	if err := s.rewrites.Save(ctx, rw); err != nil {
		s.log.Error("rewrite_save_failed", err, map[string]interface{}{
			"path":      path,
			"type":      typ,
			"entity_id": entityID,
		})
		return fmt.Errorf("rewrite: save: %w", err)
	}
	s.log.Info("rewrite_saved", map[string]interface{}{
		"path":      path,
		"type":      typ,
		"entity_id": entityID,
	})
	return nil
}

func (s *Subscriber) removeRewrite(ctx context.Context, path string) error {
	if err := s.rewrites.Delete(ctx, path); err != nil {
		s.log.Error("rewrite_delete_failed", err, map[string]interface{}{
			"path": path,
		})
		return fmt.Errorf("rewrite: delete: %w", err)
	}
	s.log.Info("rewrite_deleted", map[string]interface{}{
		"path": path,
	})
	return nil
}
