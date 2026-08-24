package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	domainintegration "github.com/akarso/shopanda/internal/domain/integration"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type mockIntegrationIdempotencyRepo struct {
	records []domainintegration.IdempotencyAdminRecord
	last    domainintegration.IdempotencyListFilter
}

func (m *mockIntegrationIdempotencyRepo) List(_ context.Context, filter domainintegration.IdempotencyListFilter) ([]domainintegration.IdempotencyAdminRecord, error) {
	m.last = filter
	return append([]domainintegration.IdempotencyAdminRecord(nil), m.records...), nil
}

func (m *mockIntegrationIdempotencyRepo) Get(_ context.Context, pluginSlug, key string) (*domainintegration.IdempotencyAdminRecord, error) {
	for i := range m.records {
		if m.records[i].PluginSlug == pluginSlug && m.records[i].IdempotencyKey == key {
			copy := m.records[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func TestIntegrationIdempotencyAdmin_List(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &mockIntegrationIdempotencyRepo{records: []domainintegration.IdempotencyAdminRecord{{
		PluginSlug:     "integrationdemo",
		IdempotencyKey: "key-1",
		Method:         "POST",
		Path:           "/api/v1/integrations/integrationdemo/orders",
		RequestHash:    "abc",
		StatusCode:     200,
		Completed:      true,
		CreatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	}}}
	h := admin.NewIntegrationIdempotencyAdminHandler(repo)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/integrations/idempotency", admin.RequirePermission(rbac.SettingsRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/integrations/idempotency?offset=0&limit=20&plugin=integrationdemo&completed=true", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.last.PluginSlug != "integrationdemo" || repo.last.Completed == nil || !*repo.last.Completed {
		t.Fatalf("filter = %+v", repo.last)
	}

	var envelope struct {
		Data struct {
			Records []map[string]interface{} `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Records) != 1 {
		t.Fatalf("records len = %d", len(envelope.Data.Records))
	}
	if _, ok := envelope.Data.Records[0]["response_body"]; ok {
		t.Fatalf("list response should omit response_body, got %+v", envelope.Data.Records[0])
	}
}

func TestIntegrationIdempotencyAdmin_ReplayRequiresCompleted(t *testing.T) {
	repo := &mockIntegrationIdempotencyRepo{records: []domainintegration.IdempotencyAdminRecord{{
		PluginSlug:     "integrationdemo",
		IdempotencyKey: "key-1",
		Completed:      false,
	}}}
	h := admin.NewIntegrationIdempotencyAdminHandler(repo)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay", admin.RequirePermission(rbac.SettingsRead)(h.Replay()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/integrations/idempotency/integrationdemo/key-1/replay", nil)
	req.SetPathValue("plugin", "integrationdemo")
	req.SetPathValue("key", "key-1")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationIdempotencyAdmin_ReplayReturnsStoredResponse(t *testing.T) {
	repo := &mockIntegrationIdempotencyRepo{records: []domainintegration.IdempotencyAdminRecord{{
		PluginSlug:     "integrationdemo",
		IdempotencyKey: "key-1",
		StatusCode:     201,
		ResponseBody:   []byte(`{"order_id":"ord-1"}`),
		Completed:      true,
	}}}
	h := admin.NewIntegrationIdempotencyAdminHandler(repo)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay", admin.RequirePermission(rbac.SettingsRead)(h.Replay()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/integrations/idempotency/integrationdemo/key-1/replay", nil)
	req.SetPathValue("plugin", "integrationdemo")
	req.SetPathValue("key", "key-1")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Replayed     bool            `json:"replayed"`
			StatusCode   int             `json:"status_code"`
			ResponseBody json.RawMessage `json:"response_body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !envelope.Data.Replayed || envelope.Data.StatusCode != 201 {
		t.Fatalf("replay payload = %+v", envelope.Data)
	}
}

func TestIntegrationIdempotencyAdmin_ReplayNonUTF8Body(t *testing.T) {
	repo := &mockIntegrationIdempotencyRepo{records: []domainintegration.IdempotencyAdminRecord{{
		PluginSlug:     "integrationdemo",
		IdempotencyKey: "key-1",
		StatusCode:     200,
		ResponseBody:   []byte{0xff, 0xfe, 0xfd},
		Completed:      true,
	}}}
	h := admin.NewIntegrationIdempotencyAdminHandler(repo)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/integrations/idempotency/{plugin}/{key}/replay", admin.RequirePermission(rbac.SettingsRead)(h.Replay()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/integrations/idempotency/integrationdemo/key-1/replay", nil)
	req.SetPathValue("plugin", "integrationdemo")
	req.SetPathValue("key", "key-1")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "non-utf8 body") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
