package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/cms"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

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

func TestContentBlockHandlerGetByTargetLayout(t *testing.T) {
	block, _ := cms.NewContentBlock("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{"headline": "Welcome"})
	repo := &mockContentBlockRepo{
		findByTargetFn: func(_ context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
			if targetType != cms.TargetTypeLayout || targetKey != "home" {
				return nil, nil
			}
			return []*cms.ContentBlock{block}, nil
		},
	}
	h := shophttp.NewContentBlockHandler(repo, menuTestPageRepo{}, cmsApp.NewBlockResolver(nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/content-blocks/{targetType}/{targetKey}", h.GetByTarget())

	req := httptest.NewRequest("GET", "/api/v1/content-blocks/layout/home", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			TargetType string `json:"target_type"`
			Blocks     []struct {
				Type string                 `json:"type"`
				Data map[string]interface{} `json:"data"`
			} `json:"blocks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.TargetType != "layout" || len(envelope.Data.Blocks) != 1 {
		t.Fatalf("unexpected response: %+v", envelope.Data)
	}
}
