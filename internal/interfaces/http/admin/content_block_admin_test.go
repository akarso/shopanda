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
	"github.com/akarso/shopanda/internal/platform/apperror"
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

func TestContentBlockAdminHandlerList(t *testing.T) {
	now := time.Now().UTC()
	block := cms.NewContentBlockFromDB("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{"headline": "Welcome"}, true, now, now)
	repo := &mockContentBlockRepo{
		listFn: func(context.Context, int, int) ([]*cms.ContentBlock, error) {
			return []*cms.ContentBlock{block}, nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/content-blocks", h.List())

	req := httptest.NewRequest("GET", "/api/v1/admin/content-blocks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("expected 1 block, got %d", len(envelope.Data))
	}
}

func TestContentBlockAdminHandlerGet_NotFound(t *testing.T) {
	repo := &mockContentBlockRepo{
		findByIDFn: func(context.Context, string) (*cms.ContentBlock, error) {
			return nil, nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/content-blocks/{id}", h.Get())

	req := httptest.NewRequest("GET", "/api/v1/admin/content-blocks/missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentBlockAdminHandlerUpdate_NotFound(t *testing.T) {
	repo := &mockContentBlockRepo{
		updateFn: func(context.Context, *cms.ContentBlock) error {
			return apperror.NotFound("content block not found")
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/content-blocks/{id}", h.Update())

	raw, _ := json.Marshal(map[string]interface{}{
		"title":      "Hero",
		"block_type": "hero",
		"config":     map[string]interface{}{"headline": "Welcome"},
		"is_active":  true,
	})
	req := httptest.NewRequest("PUT", "/api/v1/admin/content-blocks/missing", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentBlockAdminHandlerDelete_OK(t *testing.T) {
	var deletedID string
	repo := &mockContentBlockRepo{
		deleteFn: func(_ context.Context, id string) error {
			deletedID = id
			return nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/admin/content-blocks/{id}", h.Delete())

	req := httptest.NewRequest("DELETE", "/api/v1/admin/content-blocks/block-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if deletedID != "block-1" {
		t.Fatalf("expected delete of block-1, got %q", deletedID)
	}
}

func TestContentBlockAdminHandlerDelete_NotFound(t *testing.T) {
	repo := &mockContentBlockRepo{
		deleteFn: func(context.Context, string) error {
			return apperror.NotFound("content block not found")
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/admin/content-blocks/{id}", h.Delete())

	req := httptest.NewRequest("DELETE", "/api/v1/admin/content-blocks/missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentBlockAdminHandlerGetTarget(t *testing.T) {
	now := time.Now().UTC()
	block := cms.NewContentBlockFromDB("block-1", "Hero", cms.BlockTypeHero, map[string]interface{}{"headline": "Welcome"}, true, now, now)
	repo := &mockContentBlockRepo{
		findByTargetFn: func(_ context.Context, targetType cms.TargetType, targetKey string) ([]*cms.ContentBlock, error) {
			if targetType != cms.TargetTypeLayout || targetKey != "home" {
				t.Fatalf("unexpected target: %s/%s", targetType, targetKey)
			}
			return []*cms.ContentBlock{block}, nil
		},
	}
	h := admin.NewContentBlockAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/content-block-targets/{targetType}/{targetKey}", h.GetTarget())

	req := httptest.NewRequest("GET", "/api/v1/admin/content-block-targets/layout/home", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Blocks []interface{} `json:"blocks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %+v", envelope)
	}
}
