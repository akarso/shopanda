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

// mockMenuRepo duplicates the storefront menu_test.go mock — unexported, so
// it can't be shared across the http_test/admin_test package boundary
// created by the admin package split.
type mockMenuRepo struct {
	listFn       func(ctx context.Context) ([]*cms.Menu, error)
	findByIDFn   func(ctx context.Context, id string) (*cms.MenuWithItems, error)
	findByCodeFn func(ctx context.Context, code string) (*cms.MenuWithItems, error)
	saveFn       func(ctx context.Context, menu *cms.MenuWithItems) error
}

func (m *mockMenuRepo) List(ctx context.Context) ([]*cms.Menu, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockMenuRepo) FindByID(ctx context.Context, id string) (*cms.MenuWithItems, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockMenuRepo) FindByCode(ctx context.Context, code string) (*cms.MenuWithItems, error) {
	if m.findByCodeFn != nil {
		return m.findByCodeFn(ctx, code)
	}
	return nil, nil
}

func (m *mockMenuRepo) Save(ctx context.Context, menu *cms.MenuWithItems) error {
	if m.saveFn != nil {
		return m.saveFn(ctx, menu)
	}
	return nil
}

func TestMenuAdminHandlerUpdate(t *testing.T) {
	now := time.Now().UTC()
	menu := cms.NewMenuFromDB("menu-header", "header", "Header", true, now, now)
	var saved *cms.MenuWithItems

	repo := &mockMenuRepo{
		findByIDFn: func(_ context.Context, id string) (*cms.MenuWithItems, error) {
			if id != menu.ID() {
				return nil, nil
			}
			if saved != nil {
				return saved, nil
			}
			return &cms.MenuWithItems{Menu: menu, Items: nil}, nil
		},
		saveFn: func(_ context.Context, data *cms.MenuWithItems) error {
			saved = data
			return nil
		},
	}
	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewMenuAdminHandler(repo, auditor)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/menus/{id}", h.Update())

	body := map[string]interface{}{
		"title":     "Main Header",
		"is_active": true,
		"items": []map[string]interface{}{
			{"label": "Home", "link_type": "url", "link_target": "/", "position": 0},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/admin/menus/"+menu.ID(), bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if saved == nil || len(saved.Items) != 1 || saved.Items[0].Label() != "Home" {
		t.Fatalf("unexpected save: %+v", saved)
	}
}

func TestMenuAdminHandlerUpdateNotFound(t *testing.T) {
	repo := &mockMenuRepo{
		findByIDFn: func(context.Context, string) (*cms.MenuWithItems, error) {
			return nil, nil
		},
	}
	h := admin.NewMenuAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/admin/menus/{id}", h.Update())

	body := []byte(`{"title":"Header","items":[]}`)
	req := httptest.NewRequest("PUT", "/api/v1/admin/menus/missing", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "not_found" {
		t.Fatalf("unexpected error code: %+v", envelope.Error)
	}
}
