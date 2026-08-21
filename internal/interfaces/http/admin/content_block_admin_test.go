package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// mockContentBlockRepo duplicates the storefront content_block_test.go mock —
// unexported, so it can't be shared across the http_test/admin_test package
// boundary created by the admin package split.
type mockContentBlockRepo struct {
	listFn                 func(ctx context.Context, offset, limit int) ([]*cms.ContentBlock, error)
	findByIDFn             func(ctx context.Context, id string) (*cms.ContentBlock, error)
	createFn               func(ctx context.Context, block *cms.ContentBlock) error
	updateFn               func(ctx context.Context, block *cms.ContentBlock) error
	deleteFn               func(ctx context.Context, id string) error
	findByTargetFn         func(ctx context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error)
	saveTargetPlacementsFn func(ctx context.Context, targetType cms.TargetType, targetKey string, blockIDs []string) error
}

func (m *mockContentBlockRepo) List(ctx context.Context, offset, limit int) ([]*cms.ContentBlock, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}
func (m *mockContentBlockRepo) FindByID(ctx context.Context, id string) (*cms.ContentBlock, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockContentBlockRepo) Create(ctx context.Context, block *cms.ContentBlock) error {
	if m.createFn != nil {
		return m.createFn(ctx, block)
	}
	return nil
}
func (m *mockContentBlockRepo) Update(ctx context.Context, block *cms.ContentBlock) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, block)
	}
	return nil
}
func (m *mockContentBlockRepo) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}
func (m *mockContentBlockRepo) FindBlocksByTarget(ctx context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
	if m.findByTargetFn != nil {
		return m.findByTargetFn(ctx, targetType, targetKey)
	}
	return nil, nil
}
func (m *mockContentBlockRepo) FindActiveBlocksByTarget(ctx context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
	return m.FindBlocksByTarget(ctx, targetType, targetKey)
}
func (m *mockContentBlockRepo) SaveTargetPlacements(ctx context.Context, targetType cms.TargetType, targetKey string, blockIDs []string) error {
	if m.saveTargetPlacementsFn != nil {
		return m.saveTargetPlacementsFn(ctx, targetType, targetKey, blockIDs)
	}
	return nil
}

func TestContentBlockAdminHandlerCreate(t *testing.T) {
	var created *cms.ContentBlock
	repo := &mockContentBlockRepo{
		createFn: func(_ context.Context, block *cms.ContentBlock) error {
			created = block
			return nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/content-blocks", h.Create())

	body := map[string]interface{}{
		"title":      "Hero",
		"block_type": "hero",
		"config":     map[string]interface{}{"headline": "Welcome"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/admin/content-blocks", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if created == nil || created.BlockType() != cms.BlockTypeHero {
		t.Fatalf("unexpected create: %+v", created)
	}
}

func TestContentBlockAdminHandlerUpdateTarget(t *testing.T) {
	now := time.Now().UTC()
	block := cms.NewContentBlockFromDB("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{"headline": "Welcome"}, true, now, now)
	var saved []string
	repo := &mockContentBlockRepo{
		saveTargetPlacementsFn: func(_ context.Context, targetType cms.TargetType, targetKey string, blockIDs []string) error {
			saved = append([]string(nil), blockIDs...)
			return nil
		},
		findByTargetFn: func(context.Context, cms.TargetType, string) ([]*cms.ContentBlock, error) {
			return []*cms.ContentBlock{block}, nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/content-block-targets/{targetType}/{targetKey}", h.UpdateTarget())

	raw, _ := json.Marshal(map[string]interface{}{"block_ids": []string{"block-1"}})
	req := httptest.NewRequest("PUT", "/api/v1/admin/content-block-targets/layout/home", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(saved) != 1 || saved[0] != "block-1" {
		t.Fatalf("unexpected saved placements: %+v", saved)
	}
}
