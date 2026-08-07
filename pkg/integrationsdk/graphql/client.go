package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/integrationsdk"
	sdkhttp "github.com/akarso/shopanda/pkg/integrationsdk/http"
)

const defaultTimeout = 30 * time.Second

// Config configures a GraphQL integration client.
type Config struct {
	Endpoint string
	Timeout  time.Duration
	Headers  map[string]string
	Logger   integrationsdk.Logger
}

// Client performs outbound GraphQL queries over HTTP POST.
type Client struct {
	endpoint string
	http     *sdkhttp.Client
}

// Request is a GraphQL operation payload.
type Request struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// responseEnvelope is the standard GraphQL HTTP response shape.
type responseEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []Problem       `json:"errors"`
}

// Problem is one GraphQL error entry.
type Problem struct {
	Message string `json:"message"`
}

// Error indicates GraphQL errors were returned in the response envelope.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	if len(e.Problems) == 0 {
		return "integrationsdk graphql: request failed"
	}
	msgs := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		msgs[i] = p.Message
	}
	return "integrationsdk graphql: " + strings.Join(msgs, "; ")
}

// New creates a GraphQL client for endpoint.
func New(cfg Config) (*Client, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("integrationsdk graphql: endpoint required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient, err := sdkhttp.New(sdkhttp.Config{
		BaseURL: endpoint,
		Timeout: timeout,
		Headers: cfg.Headers,
		Logger:  cfg.Logger,
	})
	if err != nil {
		return nil, err
	}
	return &Client{endpoint: endpoint, http: httpClient}, nil
}

// Query executes a GraphQL query or mutation and decodes the data field into dest.
func (c *Client) Query(ctx context.Context, query string, variables map[string]interface{}, dest interface{}) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return errors.New("integrationsdk graphql: query required")
	}
	if variables == nil {
		variables = map[string]interface{}{}
	}

	var envelope responseEnvelope
	if err := c.http.DoJSON(ctx, "POST", "", nil, Request{
		Query:     query,
		Variables: variables,
	}, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return &Error{Problems: append([]Problem(nil), envelope.Errors...)}
	}
	if dest == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, dest); err != nil {
		return fmt.Errorf("integrationsdk graphql: decode data: %w", err)
	}
	return nil
}
