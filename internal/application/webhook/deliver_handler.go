package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
	"github.com/akarso/shopanda/internal/platform/ssrf"
)

const deliverTimeout = 15 * time.Second

// HTTPPoster performs outbound webhook HTTP requests.
type HTTPPoster interface {
	Post(ctx context.Context, url string, headers map[string]string, body []byte) (status int, err error)
}

// DefaultHTTPPoster posts webhook payloads using an SSRF-safe HTTP client.
type DefaultHTTPPoster struct {
	client *http.Client
}

// NewDefaultHTTPPoster creates an HTTPPoster with a delivery timeout and
// DNS-rebinding-safe dialing (private/link-local destinations rejected).
func NewDefaultHTTPPoster() *DefaultHTTPPoster {
	return NewDefaultHTTPPosterWithLookup(ssrf.DefaultLookupIP)
}

// NewDefaultHTTPPosterWithLookup is like NewDefaultHTTPPoster with an injectable resolver (tests).
func NewDefaultHTTPPosterWithLookup(lookup ssrf.LookupIPFunc) *DefaultHTTPPoster {
	return &DefaultHTTPPoster{
		client: ssrf.NewHTTPClient(deliverTimeout, lookup),
	}
}

// Post sends a POST request with the provided headers and body.
func (p *DefaultHTTPPoster) Post(ctx context.Context, rawURL string, headers map[string]string, body []byte) (int, error) {
	if err := ssrf.ValidateURL(rawURL); err != nil {
		return 0, fmt.Errorf("webhook post: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("webhook post: new request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("webhook post: do: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// DeliverHandler processes webhook.deliver jobs.
type DeliverHandler struct {
	repo    domainwebhook.Repository
	poster  HTTPPoster
	log     logger.Logger
	metrics metrics.Recorder
}

// NewDeliverHandler creates a handler for webhook.deliver jobs.
func NewDeliverHandler(repo domainwebhook.Repository, poster HTTPPoster, log logger.Logger) *DeliverHandler {
	if repo == nil {
		panic("webhook.NewDeliverHandler: nil repo")
	}
	if poster == nil {
		panic("webhook.NewDeliverHandler: nil poster")
	}
	if log == nil {
		panic("webhook.NewDeliverHandler: nil log")
	}
	return &DeliverHandler{repo: repo, poster: poster, log: log, metrics: metrics.Noop()}
}

// WithMetrics sets the metrics recorder used to record delivery outcomes.
// Optional; if never called, outcomes are simply not recorded. Returns the
// DeliverHandler for chaining.
//
// Not safe to call concurrently with Handle or with another WithMetrics
// call: the field it sets is read without synchronization on the delivery
// path. Call it once during wiring, before the handler is registered with
// the worker.
func (h *DeliverHandler) WithMetrics(m metrics.Recorder) *DeliverHandler {
	if m != nil {
		h.metrics = m
	}
	return h
}

// Type returns the job type this handler processes.
func (h *DeliverHandler) Type() string { return domainwebhook.DeliverJobType }

// Handle delivers one signed webhook payload to the configured endpoint URL.
// Skipped deliveries (inactive/unsubscribed endpoints, malformed payloads)
// are not counted as either a success or a failure — they were never attempted.
func (h *DeliverHandler) Handle(ctx context.Context, job jobs.Job) (err error) {
	skipped := false
	// permanentFailure is set when the endpoint returned a 4xx: retrying an
	// identical request against a receiver that already rejected it as a
	// client error (bad payload, wrong auth, endpoint moved) burns retry
	// budget for no chance of a different outcome, unlike a 5xx or
	// transport error, which may well succeed on retry. Handle still
	// returns nil in that case — a job the worker should mark Complete,
	// not retry via Fail — but permanentFailure keeps it out of
	// OutcomeSuccess in the metric below.
	permanentFailure := false
	defer func() {
		if skipped {
			return
		}
		outcome := metrics.OutcomeSuccess
		if err != nil || permanentFailure {
			outcome = metrics.OutcomeFailed
		}
		h.metrics.WebhookDelivery(outcome)
	}()

	endpointID, _ := job.Payload["endpoint_id"].(string)
	eventName, _ := job.Payload["event_name"].(string)
	if strings.TrimSpace(endpointID) == "" || strings.TrimSpace(eventName) == "" {
		skipped = true
		return fmt.Errorf("webhook deliver: missing endpoint_id or event_name")
	}

	endpoint, err := h.repo.FindByID(ctx, endpointID)
	if err != nil {
		// Recorded as failed, not skipped: unlike a malformed payload or an
		// inactive/unsubscribed endpoint, a repo error means we don't know
		// whether the endpoint should have received this delivery — that's
		// an operational problem (e.g. DB connectivity) worth surfacing in
		// the failure rate, not silently excluded from it.
		return fmt.Errorf("webhook deliver: find endpoint: %w", err)
	}
	if endpoint == nil || !endpoint.Active || !endpoint.Subscribed(eventName) {
		skipped = true
		h.log.Info("webhook.deliver.skipped", map[string]interface{}{
			"endpoint_id": endpointID,
			"event_name":  eventName,
			"reason":      "endpoint missing, inactive, or unsubscribed",
		})
		return nil
	}

	body, err := buildDeliveryBody(job.Payload)
	if err != nil {
		skipped = true
		return err
	}

	headers := map[string]string{
		"Content-Type":         "application/json",
		"X-Shopanda-Event":     eventName,
		"X-Shopanda-Delivery":  job.ID,
		"X-Shopanda-Signature": webhookinfra.SignBody(endpoint.Secret, body),
	}

	status, postErr := h.poster.Post(ctx, endpoint.URL, headers, body)
	if postErr != nil {
		return fmt.Errorf("webhook deliver: post: %w", postErr)
	}
	if status >= 400 && status < 500 {
		// Permanent failure: the receiver rejected this exact request as a
		// client error, so an identical retry would too. Logged as an
		// error (still worth an operator's attention — commonly a
		// misconfigured endpoint, wrong secret, or a payload shape the
		// receiver doesn't accept) but not returned, so the worker marks
		// this job Complete instead of retrying it via Fail.
		permanentFailure = true
		h.log.Error("webhook.deliver.permanent_failure", fmt.Errorf("endpoint returned status %d", status), map[string]interface{}{
			"endpoint_id": endpointID,
			"event_name":  eventName,
			"status":      status,
		})
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("webhook deliver: endpoint returned status %d", status)
	}

	h.log.Info("webhook.deliver.complete", map[string]interface{}{
		"endpoint_id": endpointID,
		"event_name":  eventName,
		"status":      status,
	})
	return nil
}

func buildDeliveryBody(payload map[string]interface{}) ([]byte, error) {
	eventID, _ := payload["event_id"].(string)
	eventName, _ := payload["event_name"].(string)
	eventSource, _ := payload["event_source"].(string)
	eventTimestamp, _ := payload["event_timestamp"].(string)
	rawData, _ := payload["event_data_json"].(string)

	var data interface{}
	if rawData != "" {
		if err := json.Unmarshal([]byte(rawData), &data); err != nil {
			return nil, fmt.Errorf("webhook deliver: unmarshal event data: %w", err)
		}
	} else {
		data = map[string]interface{}{}
	}

	envelope := map[string]interface{}{
		"id":         eventID,
		"event":      eventName,
		"source":     eventSource,
		"created_at": eventTimestamp,
		"data":       data,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("webhook deliver: marshal envelope: %w", err)
	}
	return body, nil
}
