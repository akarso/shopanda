package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/requestctx"
)

func TestContextLogger_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := NewWithWriter(&buf, "info")
	ctx := requestctx.WithRequestID(context.Background(), "req-abc")
	log := WithContext(base, ctx)

	log.Info("test.event", map[string]interface{}{
		"key": "value",
	})

	got := parseContextEntry(t, buf.String())
	if got.Context["request_id"] != "req-abc" {
		t.Errorf("request_id = %v, want %q", got.Context["request_id"], "req-abc")
	}
	if got.Context["key"] != "value" {
		t.Errorf("key = %v, want %q", got.Context["key"], "value")
	}
}

func TestContextLogger_NoRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := NewWithWriter(&buf, "info")
	log := WithContext(base, context.Background())

	log.Info("test.event", nil)

	got := parseContextEntry(t, buf.String())
	if _, ok := got.Context["request_id"]; ok {
		t.Error("request_id should not be present when not in context")
	}
}

func TestContextLogger_NilContext(t *testing.T) {
	var buf bytes.Buffer
	base := NewWithWriter(&buf, "info")
	ctx := requestctx.WithRequestID(context.Background(), "req-xyz")
	log := WithContext(base, ctx)

	log.Info("test.event", nil)

	got := parseContextEntry(t, buf.String())
	if got.Context["request_id"] != "req-xyz" {
		t.Errorf("request_id = %v, want %q", got.Context["request_id"], "req-xyz")
	}
}

func TestContextLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	base := NewWithWriter(&buf, "info")
	ctx := requestctx.WithRequestID(context.Background(), "req-w")
	log := WithContext(base, ctx)

	log.Warn("test.warn", nil)

	got := parseContextEntry(t, buf.String())
	if got.Level != "warn" {
		t.Errorf("level = %q, want %q", got.Level, "warn")
	}
	if got.Context["request_id"] != "req-w" {
		t.Errorf("request_id = %v, want %q", got.Context["request_id"], "req-w")
	}
}

func TestContextLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	base := NewWithWriter(&buf, "info")
	ctx := requestctx.WithRequestID(context.Background(), "req-e")
	log := WithContext(base, ctx)

	log.Error("test.error", errors.New("boom"), nil)

	got := parseContextEntry(t, buf.String())
	if got.Level != "error" {
		t.Errorf("level = %q, want %q", got.Level, "error")
	}
	if got.Message != "boom" {
		t.Errorf("message = %q, want %q", got.Message, "boom")
	}
	if got.Context["request_id"] != "req-e" {
		t.Errorf("request_id = %v, want %q", got.Context["request_id"], "req-e")
	}
}

func TestContextLogger_ImplementsLogger(t *testing.T) {
	base := NewWithWriter(&bytes.Buffer{}, "info")
	var _ Logger = WithContext(base, context.Background())
}

func parseContextEntry(t *testing.T, s string) entry {
	t.Helper()
	var e entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &e); err != nil {
		t.Fatalf("parse log entry: %v\nraw: %s", err, s)
	}
	return e
}
