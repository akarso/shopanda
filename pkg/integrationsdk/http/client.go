package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/integrationsdk"
)

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = 1 << 20 // 1 MiB
)

// Config configures an outbound integration HTTP client.
type Config struct {
	BaseURL          string
	Timeout          time.Duration
	Headers          map[string]string
	Logger           integrationsdk.Logger
	MaxResponseBytes int64
}

// Client performs outbound REST calls for integration plugins.
type Client struct {
	baseURL    string
	headers    map[string]string
	httpClient *http.Client
	log        integrationsdk.Logger
	maxBody    int64
}

// Response is an outbound HTTP response with a bounded body.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// OK reports whether the status code is 2xx.
func (r Response) OK() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// StatusError indicates a non-success HTTP status from an integration endpoint.
type StatusError struct {
	StatusCode int
	Body       []byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("integrationsdk http: unexpected status %d", e.StatusCode)
}

// New creates a Client from cfg.
func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, errors.New("integrationsdk http: base URL required")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("integrationsdk http: invalid base URL: %w", err)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxBody := cfg.MaxResponseBytes
	if maxBody <= 0 {
		maxBody = defaultMaxResponseBytes
	}
	headers := copyHeaders(cfg.Headers)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		log:     cfg.Logger,
		maxBody: maxBody,
	}, nil
}

// Do sends an HTTP request relative to the configured base URL.
func (c *Client) Do(ctx context.Context, method, path string, headers map[string]string, body []byte) (Response, error) {
	start := time.Now()
	method = strings.ToUpper(strings.TrimSpace(method))
	target, err := c.resolveURL(path)
	if err != nil {
		return Response{}, err
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return Response{}, fmt.Errorf("integrationsdk http: new request: %w", err)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logRequest(method, target, 0, time.Since(start), err)
		return Response{}, fmt.Errorf("integrationsdk http: do: %w", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if readErr != nil {
		c.logRequest(method, target, resp.StatusCode, time.Since(start), readErr)
		return Response{}, fmt.Errorf("integrationsdk http: read body: %w", readErr)
	}
	if int64(len(respBody)) > c.maxBody {
		err := fmt.Errorf("integrationsdk http: response body exceeds %d bytes", c.maxBody)
		c.logRequest(method, target, resp.StatusCode, time.Since(start), err)
		return Response{}, err
	}

	out := Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}
	c.logRequest(method, target, resp.StatusCode, time.Since(start), nil)
	return out, nil
}

// DoJSON sends JSON and decodes a JSON response into dest when dest is non-nil.
// Returns StatusError for non-2xx responses without decoding dest.
func (c *Client) DoJSON(ctx context.Context, method, path string, headers map[string]string, reqBody, dest interface{}) error {
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json"
	}

	var body []byte
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("integrationsdk http: marshal request: %w", err)
		}
		body = encoded
	}

	resp, err := c.Do(ctx, method, path, headers, body)
	if err != nil {
		return err
	}
	if !resp.OK() {
		return &StatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), resp.Body...)}
	}
	if dest == nil || len(resp.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Body, dest); err != nil {
		return fmt.Errorf("integrationsdk http: decode response: %w", err)
	}
	return nil
}

// Get performs GET path.
func (c *Client) Get(ctx context.Context, path string, headers map[string]string) (Response, error) {
	return c.Do(ctx, http.MethodGet, path, headers, nil)
}

// Post performs POST path with an optional body.
func (c *Client) Post(ctx context.Context, path string, headers map[string]string, body []byte) (Response, error) {
	return c.Do(ctx, http.MethodPost, path, headers, body)
}

func (c *Client) resolveURL(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return c.baseURL, nil
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path, nil
}

func (c *Client) logRequest(method, target string, status int, elapsed time.Duration, err error) {
	if c.log == nil {
		return
	}
	fields := map[string]interface{}{
		"method":   method,
		"url":      target,
		"duration": elapsed.String(),
	}
	if status > 0 {
		fields["status"] = status
	}
	if err != nil {
		c.log.Error("integrationsdk.http.request", err, fields)
		return
	}
	c.log.Info("integrationsdk.http.request", fields)
}

func copyHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
