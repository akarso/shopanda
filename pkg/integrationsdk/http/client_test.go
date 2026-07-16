package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkhttp "github.com/akarso/shopanda/pkg/integrationsdk/http"
)

type testLogger struct {
	infos  int32
	errors int32
}

func (l *testLogger) Info(string, map[string]interface{}) { atomic.AddInt32(&l.infos, 1) }
func (l *testLogger) Error(string, error, map[string]interface{}) {
	atomic.AddInt32(&l.errors, 1)
}

func TestClient_GetSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stock" || r.Method != http.MethodGet {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "secret" {
			t.Fatalf("missing api key header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	log := &testLogger{}
	client, err := sdkhttp.New(sdkhttp.Config{
		BaseURL: srv.URL,
		Timeout: time.Second,
		Headers: map[string]string{"X-Api-Key": "secret"},
		Logger:  log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Get(context.Background(), "/stock", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !resp.OK() || string(resp.Body) != `{"ok":true}` {
		t.Fatalf("resp = %+v", resp)
	}
	if atomic.LoadInt32(&log.infos) != 1 {
		t.Fatalf("expected one info log")
	}
}

func TestClient_DoJSONPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"sku":"SKU-1"`) {
			t.Fatalf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"123"}`))
	}))
	defer srv.Close()

	client, err := sdkhttp.New(sdkhttp.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var out struct {
		ID string `json:"id"`
	}
	err = client.DoJSON(context.Background(), http.MethodPost, "/items", nil, map[string]string{
		"sku": "SKU-1",
	}, &out)
	if err != nil || out.ID != "123" {
		t.Fatalf("DoJSON() = (%+v, %v)", out, err)
	}
}

func TestClient_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`upstream down`))
	}))
	defer srv.Close()

	client, err := sdkhttp.New(sdkhttp.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = client.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil, &json.RawMessage{})
	var statusErr *sdkhttp.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v", err)
	}
}

func TestClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := sdkhttp.New(sdkhttp.Config{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Get(context.Background(), "/slow", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClient_InvalidBaseURL(t *testing.T) {
	_, err := sdkhttp.New(sdkhttp.Config{BaseURL: "://bad"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_ResponseBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 32))
	}))
	defer srv.Close()

	client, err := sdkhttp.New(sdkhttp.Config{BaseURL: srv.URL, MaxResponseBytes: 16})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Get(context.Background(), "/", nil)
	if err == nil {
		t.Fatal("expected body limit error")
	}
}
