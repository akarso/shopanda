package graphql_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkgraphql "github.com/akarso/shopanda/pkg/integrationsdk/graphql"
)

func TestClient_QuerySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("content-type = %q", ct)
		}
		_, _ = w.Write([]byte(`{"data":{"product":{"name":"Widget"}}}`))
	}))
	defer srv.Close()

	client, err := sdkgraphql.New(sdkgraphql.Config{Endpoint: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var out struct {
		Product struct {
			Name string `json:"name"`
		} `json:"product"`
	}
	err = client.Query(context.Background(), `{ product { name } }`, map[string]interface{}{"slug": "widget"}, &out)
	if err != nil || out.Product.Name != "Widget" {
		t.Fatalf("Query() = (%+v, %v)", out, err)
	}
}

func TestClient_QueryGraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"not found"}]}`))
	}))
	defer srv.Close()

	client, err := sdkgraphql.New(sdkgraphql.Config{Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = client.Query(context.Background(), `{ product { name } }`, nil, &struct{}{})
	var gqlErr *sdkgraphql.Error
	if !errors.As(err, &gqlErr) || len(gqlErr.Problems) != 1 {
		t.Fatalf("err = %v", err)
	}
}

func TestClient_QueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client, err := sdkgraphql.New(sdkgraphql.Config{Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = client.Query(context.Background(), `{ ping }`, nil, &struct{}{})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestClient_QueryRequiresEndpoint(t *testing.T) {
	_, err := sdkgraphql.New(sdkgraphql.Config{})
	if err == nil {
		t.Fatal("expected endpoint error")
	}
}
