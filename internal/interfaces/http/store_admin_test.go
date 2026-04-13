package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// mockStoreAdminRepo implements store.StoreRepository for admin handler tests.
type mockStoreAdminRepo struct {
	findByIDFn func(ctx context.Context, id string) (*store.Store, error)
	findAllFn  func(ctx context.Context) ([]store.Store, error)
	createFn   func(ctx context.Context, s *store.Store) error
	updateFn   func(ctx context.Context, s *store.Store) error
}

func (m *mockStoreAdminRepo) FindByID(ctx context.Context, id string) (*store.Store, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockStoreAdminRepo) FindByCode(_ context.Context, _ string) (*store.Store, error) {
	return nil, nil
}
func (m *mockStoreAdminRepo) FindByDomain(_ context.Context, _ string) (*store.Store, error) {
	return nil, nil
}
func (m *mockStoreAdminRepo) FindDefault(_ context.Context) (*store.Store, error) { return nil, nil }
func (m *mockStoreAdminRepo) FindAll(ctx context.Context) ([]store.Store, error) {
	if m.findAllFn != nil {
		return m.findAllFn(ctx)
	}
	return nil, nil
}
func (m *mockStoreAdminRepo) Create(ctx context.Context, s *store.Store) error {
	if m.createFn != nil {
		return m.createFn(ctx, s)
	}
	return nil
}
func (m *mockStoreAdminRepo) Update(ctx context.Context, s *store.Store) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, s)
	}
	return nil
}

func storeAdminBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

func newStoreAdminRouter(h *shophttp.StoreAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/stores", h.List())
	mux.HandleFunc("POST /api/v1/admin/stores", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/stores/{id}", h.Update())
	return mux
}

func TestStoreAdmin_List_OK(t *testing.T) {
	now := time.Now()
	repo := &mockStoreAdminRepo{
		findAllFn: func(_ context.Context) ([]store.Store, error) {
			return []store.Store{
				*store.NewStoreFromDB("s-1", "de", "Germany", "EUR", "DE", "de", "shop.de", true, now, now),
			}, nil
		},
	}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	req := httptest.NewRequest("GET", "/api/v1/admin/stores", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := envelope.Data["stores"]; !ok {
		t.Error("response missing 'stores' key")
	}
}

func TestStoreAdmin_List_Empty(t *testing.T) {
	repo := &mockStoreAdminRepo{
		findAllFn: func(_ context.Context) ([]store.Store, error) {
			return nil, nil
		},
	}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	req := httptest.NewRequest("GET", "/api/v1/admin/stores", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Empty list should be [] not null.
	if !strings.Contains(rec.Body.String(), `"stores":[]`) && !strings.Contains(rec.Body.String(), `"stores": []`) {
		t.Errorf("body = %s, want stores:[]", rec.Body.String())
	}
}

func TestStoreAdmin_Create_OK(t *testing.T) {
	var created *store.Store
	repo := &mockStoreAdminRepo{
		createFn: func(_ context.Context, s *store.Store) error {
			created = s
			return nil
		},
	}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	body := `{"code":"de","name":"Germany","currency":"EUR","country":"DE","language":"de","domain":"shop.de"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/stores", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d\nbody: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("repo.Create was not called")
	}
	if created.Code != "de" {
		t.Errorf("store.Code = %q, want de", created.Code)
	}
}

func TestStoreAdmin_Create_ValidationError(t *testing.T) {
	repo := &mockStoreAdminRepo{}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	// Missing required fields.
	body := `{"code":"de"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/stores", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d\nbody: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStoreAdmin_Update_OK(t *testing.T) {
	now := time.Now()
	existing := store.NewStoreFromDB("s-1", "de", "Germany", "EUR", "DE", "de", "shop.de", false, now, now)

	var updated *store.Store
	repo := &mockStoreAdminRepo{
		findByIDFn: func(_ context.Context, id string) (*store.Store, error) {
			if id == "s-1" {
				return existing, nil
			}
			return nil, nil
		},
		updateFn: func(_ context.Context, s *store.Store) error {
			updated = s
			return nil
		},
	}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	body := `{"name":"Deutschland"}`
	req := httptest.NewRequest("PUT", "/api/v1/admin/stores/s-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil {
		t.Fatal("repo.Update was not called")
	}
	if updated.Name != "Deutschland" {
		t.Errorf("store.Name = %q, want Deutschland", updated.Name)
	}
}

func TestStoreAdmin_Update_NotFound(t *testing.T) {
	repo := &mockStoreAdminRepo{}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	body := `{"name":"Test"}`
	req := httptest.NewRequest("PUT", "/api/v1/admin/stores/missing", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStoreAdmin_Update_NormalizesCurrencyAndCountry(t *testing.T) {
	now := time.Now()
	existing := store.NewStoreFromDB("s-1", "de", "Germany", "EUR", "DE", "de", "", false, now, now)

	var updated *store.Store
	repo := &mockStoreAdminRepo{
		findByIDFn: func(_ context.Context, id string) (*store.Store, error) {
			if id == "s-1" {
				return existing, nil
			}
			return nil, nil
		},
		updateFn: func(_ context.Context, s *store.Store) error {
			updated = s
			return nil
		},
	}
	h := shophttp.NewStoreAdminHandler(repo, storeAdminBus())
	mux := newStoreAdminRouter(h)

	body := `{"currency":"usd","country":"us"}`
	req := httptest.NewRequest("PUT", "/api/v1/admin/stores/s-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil {
		t.Fatal("repo.Update was not called")
	}
	if updated.Currency != "USD" {
		t.Errorf("Currency = %q, want USD (uppercased)", updated.Currency)
	}
	if updated.Country != "US" {
		t.Errorf("Country = %q, want US (uppercased)", updated.Country)
	}
}
