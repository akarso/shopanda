package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/rbac"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type memoryWebhookRepo struct {
	items map[string]domainwebhook.Endpoint
}

func (m *memoryWebhookRepo) List(_ context.Context) ([]domainwebhook.Endpoint, error) {
	out := make([]domainwebhook.Endpoint, 0, len(m.items))
	for _, ep := range m.items {
		out = append(out, ep)
	}
	return out, nil
}

func (m *memoryWebhookRepo) ListActive(ctx context.Context) ([]domainwebhook.Endpoint, error) {
	all, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domainwebhook.Endpoint, 0)
	for _, ep := range all {
		if ep.Active {
			out = append(out, ep)
		}
	}
	return out, nil
}

func (m *memoryWebhookRepo) FindByID(_ context.Context, id string) (*domainwebhook.Endpoint, error) {
	if m.items == nil {
		return nil, nil
	}
	ep, ok := m.items[id]
	if !ok {
		return nil, nil
	}
	copy := ep
	return &copy, nil
}

func (m *memoryWebhookRepo) Create(_ context.Context, endpoint *domainwebhook.Endpoint) error {
	if m.items == nil {
		m.items = map[string]domainwebhook.Endpoint{}
	}
	m.items[endpoint.ID] = *endpoint
	return nil
}

func (m *memoryWebhookRepo) Update(_ context.Context, endpoint *domainwebhook.Endpoint) error {
	if _, ok := m.items[endpoint.ID]; !ok {
		return nil
	}
	m.items[endpoint.ID] = *endpoint
	return nil
}

func (m *memoryWebhookRepo) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

func TestWebhookEndpointAdmin_CreateReturnsSecret(t *testing.T) {
	repo := &memoryWebhookRepo{items: map[string]domainwebhook.Endpoint{}}
	h := shophttp.NewWebhookEndpointAdminHandler(webhookApp.NewService(repo))

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/webhooks", shophttp.RequirePermission(rbac.SettingsWrite)(h.Create()))

	body := `{"url":"https://example.com/hook","events":["order.paid"],"active":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Secret   string `json:"secret"`
			Endpoint struct {
				ID string `json:"id"`
			} `json:"endpoint"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Secret == "" || envelope.Data.Endpoint.ID == "" {
		t.Fatalf("response = %+v", envelope.Data)
	}
}

func TestWebhookEndpointAdmin_Catalog(t *testing.T) {
	h := shophttp.NewWebhookEndpointAdminHandler(webhookApp.NewService(&memoryWebhookRepo{}))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/webhooks/events", shophttp.RequirePermission(rbac.SettingsRead)(h.Catalog()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/webhooks/events", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), order.EventOrderPaid) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
