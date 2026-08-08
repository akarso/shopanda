package webhook_test

import (
	"context"
	"strings"
	"testing"

	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
)

type memWebhookRepo struct {
	byID map[string]*domainwebhook.Endpoint
}

func (m *memWebhookRepo) List(context.Context) ([]domainwebhook.Endpoint, error) {
	out := make([]domainwebhook.Endpoint, 0, len(m.byID))
	for _, ep := range m.byID {
		out = append(out, *ep)
	}
	return out, nil
}
func (m *memWebhookRepo) ListActive(context.Context) ([]domainwebhook.Endpoint, error) {
	return m.List(context.Background())
}
func (m *memWebhookRepo) FindByID(_ context.Context, id string) (*domainwebhook.Endpoint, error) {
	return m.byID[id], nil
}
func (m *memWebhookRepo) Create(_ context.Context, ep *domainwebhook.Endpoint) error {
	if m.byID == nil {
		m.byID = map[string]*domainwebhook.Endpoint{}
	}
	cp := *ep
	m.byID[ep.ID] = &cp
	return nil
}
func (m *memWebhookRepo) Update(_ context.Context, ep *domainwebhook.Endpoint) error {
	cp := *ep
	m.byID[ep.ID] = &cp
	return nil
}
func (m *memWebhookRepo) Delete(_ context.Context, id string) error {
	delete(m.byID, id)
	return nil
}

func TestService_CreateRejectsPrivateLiteralURL(t *testing.T) {
	svc := webhookApp.NewService(&memWebhookRepo{})
	_, err := svc.Create(context.Background(), webhookApp.CreateInput{
		URL:    "https://127.0.0.1/hooks",
		Events: []string{"order.paid"},
		Active: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want SSRF rejection", err)
	}
}
