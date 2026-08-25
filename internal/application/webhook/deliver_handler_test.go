package webhook_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/order"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

type recordingMetrics struct {
	webhookOutcomes []string
}

func (m *recordingMetrics) HTTPRequest(string, string, string, time.Duration) {}
func (m *recordingMetrics) CheckoutResult(string)                             {}
func (m *recordingMetrics) JobFailure(string)                                 {}
func (m *recordingMetrics) WebhookDelivery(outcome string) {
	m.webhookOutcomes = append(m.webhookOutcomes, outcome)
}

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
	mu       sync.Mutex
	jobs     []jobs.Job
	enqueued chan jobs.Job
}

func (q *recordingQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	q.jobs = append(q.jobs, job)
	q.mu.Unlock()
	if q.enqueued != nil {
		q.enqueued <- job
	}
	return nil
}

func (q *recordingQueue) snapshot() []jobs.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]jobs.Job, len(q.jobs))
	copy(out, q.jobs)
	return out
}

func (*recordingQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (*recordingQueue) Complete(context.Context, string) error     { return nil }
func (*recordingQueue) Fail(context.Context, string, error) error  { return nil }

type stubPoster struct {
	lastURL      string
	lastHeaders  map[string]string
	lastBody     []byte
	status       int
	transportErr error
}

func (p *stubPoster) Post(_ context.Context, url string, headers map[string]string, body []byte) (int, error) {
	p.lastURL = url
	p.lastHeaders = headers
	p.lastBody = body
	if p.transportErr != nil {
		return 0, p.transportErr
	}
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
	queue := &recordingQueue{enqueued: make(chan jobs.Job, 1)}
	dispatcher := webhookApp.NewDispatcher(repo, queue, logger.New("error"))
	bus := event.NewBus(logger.New("error"))
	dispatcher.Register(bus)

	if err := bus.Publish(context.Background(), event.New(order.EventOrderPaid, "test", order.OrderStatusChangedData{
		OrderID: "ord-1",
	})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case job := <-queue.enqueued:
		if job.Type != domainwebhook.DeliverJobType {
			t.Fatalf("job type = %q", job.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for webhook enqueue")
	}
	jobsSnapshot := queue.snapshot()
	if len(jobsSnapshot) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobsSnapshot))
	}
}

func TestDeliverHandler_PostsSignedPayload(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {
				ID:     "ep-1",
				URL:    "https://hooks.example.com/hook",
				Secret: "top-secret",
				Events: []string{order.EventOrderPaid},
				Active: true,
			},
		},
	}
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
	if poster.lastURL != "https://hooks.example.com/hook" {
		t.Fatalf("URL = %q", poster.lastURL)
	}
	want := webhookinfra.SignBody("top-secret", poster.lastBody)
	if poster.lastHeaders["X-Shopanda-Signature"] != want {
		t.Fatalf("signature = %q, want %q", poster.lastHeaders["X-Shopanda-Signature"], want)
	}
}

func TestDefaultHTTPPoster_RejectsLoopbackLiteral(t *testing.T) {
	poster := webhookApp.NewDefaultHTTPPoster()
	_, err := poster.Post(context.Background(), "https://127.0.0.1/hook", nil, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want loopback rejection", err)
	}
}

func TestDefaultHTTPPoster_RejectsPrivateDNS(t *testing.T) {
	poster := webhookApp.NewDefaultHTTPPosterWithLookup(func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3")}, nil
	})
	_, err := poster.Post(context.Background(), "https://internal.example/hook", map[string]string{
		"Content-Type": "application/json",
	}, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("err=%v, want private DNS rejection", err)
	}
}

func TestDefaultHTTPPoster_RejectsMetadataIP(t *testing.T) {
	poster := webhookApp.NewDefaultHTTPPoster()
	_, err := poster.Post(context.Background(), "https://169.254.169.254/latest/meta-data", nil, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err=%v, want metadata rejection", err)
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
				Events: []string{order.EventOrderPaid},
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

func TestDeliverHandler_SkipsUnsubscribedEvent(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {
				ID:     "ep-1",
				URL:    "https://example.com/hook",
				Secret: "top-secret",
				Events: []string{"order.created"},
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
	if poster.lastURL != "" {
		t.Fatal("expected delivery to be skipped for unsubscribed event")
	}
}

func TestDeliverHandler_RetriesOnNon2xx(t *testing.T) {
	poster := &stubPoster{status: http.StatusInternalServerError}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
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

func TestDeliverHandler_WithMetrics_RecordsFailureOnTransportError(t *testing.T) {
	poster := &stubPoster{transportErr: fmt.Errorf("dial tcp: connection refused")}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
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
		t.Fatal("expected delivery failure for transport error")
	}
	if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeFailed {
		t.Errorf("webhookOutcomes = %v, want [failed]", m.webhookOutcomes)
	}
}

// TestDeliverHandler_4xxDoesNotRetry pins the fix classifying a 4xx
// response as a permanent failure: retrying an identical request against
// a receiver that already rejected it as a client error burns retry
// budget for no chance of a different outcome. Handle must return nil
// (the worker marks the job Complete, not retried via Fail) while still
// counting it as a failed delivery in metrics — a 4xx is not the same as
// a skipped delivery (inactive/unsubscribed endpoint), which is never
// attempted at all.
func TestDeliverHandler_4xxDoesNotRetry(t *testing.T) {
	poster := &stubPoster{status: http.StatusBadRequest}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
	err := h.Handle(context.Background(), jobs.Job{
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
	})
	if err != nil {
		t.Fatalf("Handle: %v, want nil — a 4xx must not trigger a worker retry", err)
	}
	if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeFailed {
		t.Errorf("webhookOutcomes = %v, want [failed] — still a real failed delivery for observability, even though it doesn't retry", m.webhookOutcomes)
	}
}

// TestDeliverHandler_5xxStillRetries is the retryable-error sibling of the
// 4xx test above — confirms the new classification didn't accidentally
// stop retrying transient server errors, which is exactly what
// TestDeliverHandler_RetriesOnNon2xx already pins; this only adds the
// metrics-outcome check that test doesn't make.
func TestDeliverHandler_5xxStillRetries(t *testing.T) {
	poster := &stubPoster{status: http.StatusServiceUnavailable}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
	err := h.Handle(context.Background(), jobs.Job{
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
	})
	if err == nil {
		t.Fatal("Handle: nil, want an error — a 5xx must still trigger a worker retry")
	}
	if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeFailed {
		t.Errorf("webhookOutcomes = %v, want [failed]", m.webhookOutcomes)
	}
}

// TestDeliverHandler_TransientStatusesStillRetry pins the fix that not
// every 4xx is a permanent failure: 408 (Request Timeout), 425 (Too
// Early), and 429 (Too Many Requests) all signal a transient condition
// where an identical retry can succeed, unlike a genuine client error
// such as 400 or 404 (see TestDeliverHandler_4xxDoesNotRetry).
func TestDeliverHandler_TransientStatusesStillRetry(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			poster := &stubPoster{status: status}
			repo := &stubWebhookRepo{
				byID: map[string]*domainwebhook.Endpoint{
					"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
				},
			}
			m := &recordingMetrics{}
			h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
			err := h.Handle(context.Background(), jobs.Job{
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
			})
			if err == nil {
				t.Fatalf("Handle: nil, want an error — status %d must still trigger a worker retry", status)
			}
			if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeFailed {
				t.Errorf("webhookOutcomes = %v, want [failed]", m.webhookOutcomes)
			}
		})
	}
}

func TestDeliverHandler_WithMetrics_RecordsSuccess(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
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
	if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeSuccess {
		t.Errorf("webhookOutcomes = %v, want [success]", m.webhookOutcomes)
	}
}

func TestDeliverHandler_WithMetrics_RecordsFailureOnNon2xx(t *testing.T) {
	poster := &stubPoster{status: http.StatusInternalServerError}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{order.EventOrderPaid}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
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
		t.Fatal("expected delivery failure")
	}
	if len(m.webhookOutcomes) != 1 || m.webhookOutcomes[0] != metrics.OutcomeFailed {
		t.Errorf("webhookOutcomes = %v, want [failed]", m.webhookOutcomes)
	}
}

func TestDeliverHandler_WithMetrics_SkippedDeliveryNotCounted(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{"order.created"}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
	if err := h.Handle(context.Background(), jobs.Job{
		ID:   "job-1",
		Type: domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{
			"endpoint_id":     "ep-1",
			"event_name":      order.EventOrderPaid, // unsubscribed -> skipped
			"event_id":        "evt-1",
			"event_source":    "test",
			"event_timestamp": "2026-06-17T12:00:00Z",
			"event_data_json": `{}`,
		},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(m.webhookOutcomes) != 0 {
		t.Errorf("webhookOutcomes = %v, want none for a skipped delivery", m.webhookOutcomes)
	}
}

func TestDeliverHandler_WithMetrics_MalformedEventDataJSONNotCounted(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	repo := &stubWebhookRepo{
		byID: map[string]*domainwebhook.Endpoint{
			"ep-1": {ID: "ep-1", URL: "https://example.com/hook", Secret: "s", Events: []string{"order.paid"}, Active: true},
		},
	}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(repo, poster, logger.New("error")).WithMetrics(m)
	err := h.Handle(context.Background(), jobs.Job{
		ID:   "job-1",
		Type: domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{
			"endpoint_id":     "ep-1",
			"event_name":      order.EventOrderPaid,
			"event_id":        "evt-1",
			"event_source":    "test",
			"event_timestamp": "2026-06-17T12:00:00Z",
			"event_data_json": "{not valid json",
		},
	})
	if err == nil {
		t.Fatal("expected error for malformed event_data_json")
	}
	if poster.lastURL != "" {
		t.Errorf("poster.lastURL = %q, want empty (never attempted)", poster.lastURL)
	}
	if len(m.webhookOutcomes) != 0 {
		t.Errorf("webhookOutcomes = %v, want none for malformed event_data_json (never attempted)", m.webhookOutcomes)
	}
}

func TestDeliverHandler_WithMetrics_MalformedPayloadNotCounted(t *testing.T) {
	poster := &stubPoster{status: http.StatusOK}
	m := &recordingMetrics{}
	h := webhookApp.NewDeliverHandler(&stubWebhookRepo{}, poster, logger.New("error")).WithMetrics(m)
	err := h.Handle(context.Background(), jobs.Job{
		ID:      "job-1",
		Type:    domainwebhook.DeliverJobType,
		Payload: map[string]interface{}{"endpoint_id": ""},
	})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
	if len(m.webhookOutcomes) != 0 {
		t.Errorf("webhookOutcomes = %v, want none for malformed payload (never attempted)", m.webhookOutcomes)
	}
}
