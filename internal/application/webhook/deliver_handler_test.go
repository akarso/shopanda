package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/order"
	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type stubWebhookRepo struct {
	endpoints []domainwebhook.Endpoint
	byID      map[string]*domainwebhook.Endpoint
}

func (s *stubWebhookRepo) List(context.Context) ([]domainwebhook.Endpoint, error) {
	return s.endpoints, nil
}

func (s *stubWebhookRepo) ListActive(context.Context) ([]domainwebhook.Endpoint, error) {
	out := make([]domainwebhook.Endpoint, 0)
	for _, ep := range s.endpoints {
		if ep.Active {
			out = append(out, ep)
		}
	}
	return out, nil
}

func (s *stubWebhookRepo) FindByID(_ context.Context, id string) (*domainwebhook.Endpoint, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubWebhookRepo) Create(context.Context, *domainwebhook.Endpoint) error { return nil }
func (s *stubWebhookRepo) Update(context.Context, *domainwebhook.Endpoint) error { return nil }
func (s *stubWebhookRepo) Delete(context.Context, string) error                  { return nil }

type recordingQueue struct {
	jobs []jobs.Job
}

func (q *recordingQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.jobs = append(q.jobs, job)
	return nil
}

func (recordingQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (recordingQueue) Complete(context.Context, string) error      { return nil }
func (recordingQueue) Fail(context.Context, string, error) error   { return nil }

type stubPoster struct {
	lastURL     string
	lastHeaders map[string]string
	lastBody    []byte
	status      int
}

func (p *stubPoster) Post(_ context.Context, url string, headers map[string]string, body []byte) (int, error) {
	p.lastURL = url
	p.lastHeaders = headers
	p.lastBody = body
	if p.status == 0 {
		p.status = http.StatusOK
	}
	return p.status, nil
}

func TestDispatcher_EnqueuesMatchingEndpoints(t *testing.T) {
	repo := &stubWebhookRepo{
		endpoints: []domainwebhook.Endpoint{{
			ID:     "ep-1",
			URL:    "https://example.com/hook",
			Secret: "secret",
			Events: []string{order.EventOrderPaid},
			Active: true,
		}},
	}
	queue := &recordingQueue{}
	dispatcher := webhookApp.NewDispatcher(repo, queue, logger.New("error"))
	bus := event.NewBus(logger.New("error"))
	dispatcher.Register(bus)

	if err := bus.Publish(context.Background(), event.New(order.EventOrderPaid, "test", order.OrderStatusChangedData{
		OrderID: "ord-1",
	})); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(queue.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(queue.jobs))
	}
	if queue.jobs[0].Type != domainwebhook.DeliverJobType {
		t.Fatalf("job type = %q", queue.jobs[0].Type)
	}
}

func TestDeliverHandler_PostsSignedPayload(t *testing.T) {
	var receivedSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Shopanda-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {
				ID:     "ep-1",
				URL:    srv.URL,
				Secret: "top-secret",
				Active: true,
			},
		},
	}
	poster := webhookApp.NewDefaultHTTPPoster()
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error"))

	err := h.Handle(context.Background(), jobs.Job{
		ID:   "job-1",
		Type: domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{
			"endpoint_id":     "ep-1",
			"event_id":        "evt-1",
			"event_name":      order.EventOrderPaid,
			"event_source":    "test",
			"event_timestamp": "2026-06-17T12:00:00Z",
			"event_data_json": `{"order_id":"ord-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if receivedSig == "" {
		t.Fatal("expected signature header")
	}
}

func TestDeliverHandler_VerifiesSignature(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {
				ID:     "ep-1",
				URL:    "https://example.com/hook",
				Secret: "top-secret",
				Active: true,
			},
		},
	}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error"))
	if err := h.Handle(context.Background(), jobs.Job{
		ID:   "job-1",
		Type: domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{
			"endpoint_id":     "ep-1",
			"event_name":      order.EventOrderPaid,
			"event_id":        "evt-1",
			"event_source":    "test",
			"event_timestamp": "2026-06-17T12:00:00Z",
			"event_data_json": `{}`,
		},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	want := webhookinfra.SignBody("top-secret", poster.lastBody)
	if poster.lastHeaders["X-Shopanda-Signature"] != want {
		t.Fatalf("signature = %q, want %q", poster.lastHeaders["X-Shopanda-Signature"], want)
	}
}

func TestDeliverHandler_RetriesOnNon2xx(t *testing.T) {
	poster := &stubPoster{status: http.StatusInternalServerError}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Active: true},
		},
	}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error"))
	if err := h.Handle(context.Background(), jobs.Job{
		ID:   "job-1",
		Type: domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{
			"endpoint_id":     "ep-1",
			"event_name":      order.EventOrderPaid,
			"event_id":        "evt-1",
			"event_source":    "test",
			"event_timestamp": "2026-06-17T12:00:00Z",
			"event_data_json": `{}`,
		},
	}); err == nil {
		t.Fatal("expected delivery failure for retry")
	}
}
