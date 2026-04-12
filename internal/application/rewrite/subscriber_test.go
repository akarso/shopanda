package rewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/rewrite"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- fakes ---

type fakeRewriteRepo struct {
	saved    []*routing.URLRewrite
	deleted  []string
	existing *routing.URLRewrite
	err      error
}

func (f *fakeRewriteRepo) FindByPath(_ context.Context, _ string) (*routing.URLRewrite, error) {
	return f.existing, nil
}

func (f *fakeRewriteRepo) Save(_ context.Context, rw *routing.URLRewrite) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, rw)
	return nil
}

func (f *fakeRewriteRepo) Delete(_ context.Context, path string) error {
	f.deleted = append(f.deleted, path)
	return nil
}

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

func TestHandlePageCreated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageCreated, "test", cms.PageCreatedData{
		PageID: "page-1",
		Slug:   "about-us",
		Title:  "About Us",
	})

	err := sub.HandlePageCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/about-us" {
		t.Errorf("path = %q, want %q", rw.Path(), "/about-us")
	}
	if rw.Type() != "page" {
		t.Errorf("type = %q, want %q", rw.Type(), "page")
	}
	if rw.EntityID() != "page-1" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "page-1")
	}
}

func TestHandlePageUpdated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageUpdated, "test", cms.PageUpdatedData{
		PageID: "page-2",
		Slug:   "contact",
		Title:  "Contact",
		Active: true,
	})

	err := sub.HandlePageUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	rw := repo.saved[0]
	if rw.Path() != "/contact" {
		t.Errorf("path = %q, want %q", rw.Path(), "/contact")
	}
	if rw.Type() != "page" {
		t.Errorf("type = %q, want %q", rw.Type(), "page")
	}
	if rw.EntityID() != "page-2" {
		t.Errorf("entity_id = %q, want %q", rw.EntityID(), "page-2")
	}
}

func TestHandlePageUpdated_SlugChanged(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageUpdated, "test", cms.PageUpdatedData{
		PageID:  "page-2",
		Slug:    "contact-us",
		OldSlug: "contact",
		Title:   "Contact Us",
		Active:  true,
	})

	err := sub.HandlePageUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(repo.deleted))
	}
	if repo.deleted[0] != "/contact" {
		t.Errorf("deleted path = %q, want %q", repo.deleted[0], "/contact")
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
	}
	if repo.saved[0].Path() != "/contact-us" {
		t.Errorf("saved path = %q, want %q", repo.saved[0].Path(), "/contact-us")
	}
}

func TestHandlePageUpdated_Deactivated(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageUpdated, "test", cms.PageUpdatedData{
		PageID: "page-2",
		Slug:   "contact",
		Title:  "Contact",
		Active: false,
	})

	err := sub.HandlePageUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0 (deactivated page should remove rewrite)", len(repo.saved))
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(repo.deleted))
	}
	if repo.deleted[0] != "/contact" {
		t.Errorf("deleted path = %q, want %q", repo.deleted[0], "/contact")
	}
}

func TestHandlePageDeleted(t *testing.T) {
	repo := &fakeRewriteRepo{}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageDeleted, "test", cms.PageDeletedData{
		PageID: "page-3",
		Slug:   "old-page",
	})

	err := sub.HandlePageDeleted(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("deleted = %d, want 1", len(repo.deleted))
	}
	if repo.deleted[0] != "/old-page" {
		t.Errorf("deleted path = %q, want %q", repo.deleted[0], "/old-page")
	}
}

func TestSaveRewrite_CollisionRejected(t *testing.T) {
	existing := routing.NewURLRewriteFromDB("/about-us", "product", "prod-99")
	repo := &fakeRewriteRepo{existing: existing}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageCreated, "test", cms.PageCreatedData{
		PageID: "page-1",
		Slug:   "about-us",
		Title:  "About Us",
		Active: true,
	})

	err := sub.HandlePageCreated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected collision error")
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0 (collision should prevent save)", len(repo.saved))
	}
}

func TestSaveRewrite_SameOwnerAllowed(t *testing.T) {
	existing := routing.NewURLRewriteFromDB("/about-us", "page", "page-1")
	repo := &fakeRewriteRepo{existing: existing}
	sub := rewrite.NewSubscriber(repo, &fakeLog{})

	evt := event.New(cms.EventPageUpdated, "test", cms.PageUpdatedData{
		PageID: "page-1",
		Slug:   "about-us",
		Title:  "About Us Updated",
		Active: true,
	})

	err := sub.HandlePageUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved = %d, want 1", len(repo.saved))
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
	if bus.Handlers(cms.EventPageCreated) != 1 {
		t.Error("expected 1 handler for cms.page.created")
	}
	if bus.Handlers(cms.EventPageUpdated) != 1 {
		t.Error("expected 1 handler for cms.page.updated")
	}
	if bus.Handlers(cms.EventPageDeleted) != 1 {
		t.Error("expected 1 handler for cms.page.deleted")
	}
}
