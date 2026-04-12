package rewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/rewrite"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- fakes ---

type fakeRewriteRepo struct {
	saved []*routing.URLRewrite
	err   error
}

func (f *fakeRewriteRepo) FindByPath(_ context.Context, _ string) (*routing.URLRewrite, error) {
	return nil, nil
}

func (f *fakeRewriteRepo) Save(_ context.Context, rw *routing.URLRewrite) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, rw)
	return nil
}

func (f *fakeRewriteRepo) Delete(_ context.Context, _ string) error { return nil }

type fakeLog struct{}

func (f *fakeLog) Info(_ string, _ map[string]interface{})           {}
func (f *fakeLog) Warn(_ string, _ map[string]interface{})           {}
func (f *fakeLog) Error(_ string, _ error, _ map[string]interface{}) {}

// --- tests ---

func TestHandleProductCreated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(catalog.EventProductCreated, "test", catalog.ProductCreatedData{
		ProductID: "prod-1",
		Name:      "Nike Air Max",
		Slug:      "nike-air-max",
		Status:    catalog.StatusActive,
	})

	err := sub.HandleProductCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/nike-air-max" {
		t.Errorf("path = %q, want %q", rw.Path(), "/nike-air-max")
	}
	if rw.Type() != "product" {
		t.Errorf("type = %q, want %q", rw.Type(), "product")
	}
	if rw.EntityID() != "prod-1" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "prod-1")
	}
}

func TestHandleProductUpdated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{
		ProductID: "prod-2",
		Name:      "Updated Shoe",
		Slug:      "updated-shoe",
		Status:    catalog.StatusActive,
	})

	err := sub.HandleProductUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/updated-shoe" {
		t.Errorf("path = %q, want %q", rw.Path(), "/updated-shoe")
	}
	if rw.Type() != "product" {
		t.Errorf("type = %q, want %q", rw.Type(), "product")
	}
	if rw.EntityID() != "prod-2" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "prod-2")
	}
}

func TestHandleCategoryCreated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(catalog.EventCategoryCreated, "test", catalog.CategoryCreatedData{
		CategoryID: "cat-1",
		Name:       "Shoes",
		Slug:       "shoes",
	})

	err := sub.HandleCategoryCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/shoes" {
		t.Errorf("path = %q, want %q", rw.Path(), "/shoes")
	}
	if rw.Type() != "category" {
		t.Errorf("type = %q, want %q", rw.Type(), "category")
	}
	if rw.EntityID() != "cat-1" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "cat-1")
	}
}

func TestHandleCategoryUpdated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(catalog.EventCategoryUpdated, "test", catalog.CategoryUpdatedData{
		CategoryID: "cat-2",
		Name:       "Apparel",
		Slug:       "apparel",
	})

	err := sub.HandleCategoryUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/apparel" {
		t.Errorf("path = %q, want %q", rw.Path(), "/apparel")
	}
	if rw.Type() != "category" {
		t.Errorf("type = %q, want %q", rw.Type(), "category")
	}
	if rw.EntityID() != "cat-2" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "cat-2")
	}
}

func TestHandleProductCreated_WrongDataType(t *testing.T) {
	sub := rewrite.NewSubscriber(&fakeRewriteRepo{}, &fakeLog{})

	evt := event.New(catalog.EventProductCreated, "test", "not a struct")
	err := sub.HandleProductCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for wrong data type")
	}
}

func TestHandleProductCreated_RepoError(t *testing.T) {
	repo := &fakeRewriteRepo{err: errors.New("db down")}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(catalog.EventProductCreated, "test", catalog.ProductCreatedData{
		ProductID: "prod-1",
		Slug:      "slug",
	})

	err := sub.HandleProductCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error when repo fails")
	}
}

func TestRegister(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})
	bus := event.NewBus(&fakeLog{})
	sub.Register(bus)

	if bus.Handlers(catalog.EventProductCreated) != 1 {
		t.Error("expected 1 handler for product.created")
	}
	if bus.Handlers(catalog.EventProductUpdated) != 1 {
		t.Error("expected 1 handler for product.updated")
	}
	if bus.Handlers(catalog.EventCategoryCreated) != 1 {
		t.Error("expected 1 handler for category.created")
	}
	if bus.Handlers(catalog.EventCategoryUpdated) != 1 {
		t.Error("expected 1 handler for category.updated")
	}
}
