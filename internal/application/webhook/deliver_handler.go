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

	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/domain/jobs"
	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const deliverTimeout = 15 * time.Second

// HTTPPoster performs outbound webhook HTTP requests.
type HTTPPoster interface {
	Post(ctx context.Context, url string, headers map[string]string, body []byte) (status int, err error)
}

// DefaultHTTPPoster posts webhook payloads using net/http.
type DefaultHTTPPoster struct {
	client *http.Client
}

// NewDefaultHTTPPoster creates an HTTPPoster with a delivery timeout.
func NewDefaultHTTPPoster() *DefaultHTTPPoster {
	return &DefaultHTTPPoster{
		client: &http.Client{
			Timeout: deliverTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Post sends a POST request with the provided headers and body.
func (p *DefaultHTTPPoster) Post(ctx context.Context, url string, headers map[string]string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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
	repo   domainwebhook.Repository
	poster HTTPPoster
	log    logger.Logger
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
	return &DeliverHandler{repo: repo, poster: poster, log: log}
}

// Type returns the job type this handler processes.
func (h *DeliverHandler) Type() string { return domainwebhook.DeliverJobType }

// Handle delivers one signed webhook payload to the configured endpoint URL.
func (h *DeliverHandler) Handle(ctx context.Context, job jobs.Job) error {
	endpointID, _ := job.Payload["endpoint_id"].(string)
	eventName, _ := job.Payload["event_name"].(string)
	if strings.TrimSpace(endpointID) == "" || strings.TrimSpace(eventName) == "" {
		return fmt.Errorf("webhook deliver: missing endpoint_id or event_name")
	}

	endpoint, err := h.repo.FindByID(ctx, endpointID)
	if err != nil {
		return fmt.Errorf("webhook deliver: find endpoint: %w", err)
	}
	if endpoint == nil || !endpoint.Active || !endpoint.Subscribed(eventName) {
		h.log.Info("webhook.deliver.skipped", map[string]interface{}{
			"endpoint_id": endpointID,
			"event_name":  eventName,
			"reason":      "endpoint missing, inactive, or unsubscribed",
		})
		return nil
	}

	body, err := buildDeliveryBody(job.Payload)
	if err != nil {
		return err
	}

	headers := map[string]string{
		"Content-Type":        "application/json",
		"X-Shopanda-Event":    eventName,
		"X-Shopanda-Delivery": job.ID,
		"X-Shopanda-Signature": webhookinfra.SignBody(endpoint.Secret, body),
	}

	status, err := h.poster.Post(ctx, endpoint.URL, headers, body)
	if err != nil {
		return err
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
