package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInfo_WritesJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Info("order.created", map[string]interface{}{
		"order_id": "ord_123",
	})

	got := parseEntry(t, buf.String())
	if got.Level != "info" {
		t.Errorf("level = %q, want %q", got.Level, "info")
	}
	if got.Event != "order.created" {
		t.Errorf("event = %q, want %q", got.Event, "order.created")
	}
	if got.Context["order_id"] != "ord_123" {
		t.Errorf("context.order_id = %v, want %q", got.Context["order_id"], "ord_123")
	}
	if got.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestWarn_WritesJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Warn("inventory.low", map[string]interface{}{
		"variant_id": "var_1",
		"quantity":   float64(2),
	})

	got := parseEntry(t, buf.String())
	if got.Level != "warn" {
		t.Errorf("level = %q, want %q", got.Level, "warn")
	}
	if got.Event != "inventory.low" {
		t.Errorf("event = %q, want %q", got.Event, "inventory.low")
	}
}

func TestError_IncludesMessage(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Error("payment.failed", errors.New("card declined"), map[string]interface{}{
		"payment_id": "pay_1",
	})

	got := parseEntry(t, buf.String())
	if got.Level != "error" {
		t.Errorf("level = %q, want %q", got.Level, "error")
	}
	if got.Message != "card declined" {
		t.Errorf("message = %q, want %q", got.Message, "card declined")
	}
}

func TestError_NilError(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Error("something.failed", nil, nil)

	got := parseEntry(t, buf.String())
	if got.Message != "" {
		t.Errorf("message = %q, want empty", got.Message)
	}
}

func TestLevelFiltering_WarnLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "warn")

	log.Info("should.be.suppressed", nil)
	if buf.Len() != 0 {
		t.Error("info should be suppressed at warn level")
	}

	log.Warn("should.appear", nil)
	if buf.Len() == 0 {
		t.Error("warn should not be suppressed at warn level")
	}
}

func TestLevelFiltering_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "error")

	log.Info("suppressed", nil)
	log.Warn("suppressed", nil)
	if buf.Len() != 0 {
		t.Error("info and warn should be suppressed at error level")
	}

	log.Error("visible", errors.New("fail"), nil)
	if buf.Len() == 0 {
		t.Error("error should not be suppressed at error level")
	}
}

func TestNilContext(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Info("app.started", nil)

	got := parseEntry(t, buf.String())
	if got.Event != "app.started" {
		t.Errorf("event = %q, want %q", got.Event, "app.started")
	}
}

func TestParseLevel_Defaults(t *testing.T) {
	if ParseLevel("info") != LevelInfo {
		t.Error("info should parse to LevelInfo")
	}
	if ParseLevel("warn") != LevelWarn {
		t.Error("warn should parse to LevelWarn")
	}
	if ParseLevel("error") != LevelError {
		t.Error("error should parse to LevelError")
	}
	if ParseLevel("unknown") != LevelInfo {
		t.Error("unknown should default to LevelInfo")
	}
}

func TestMultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info")

	log.Info("first", nil)
	log.Info("second", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2", len(lines))
	}
}

func TestInterface(t *testing.T) {
	var _ Logger = New("info")
}

// parseEntry decodes the first JSON line from s.
func parseEntry(t *testing.T, s string) entry {
	t.Helper()
	var e entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &e); err != nil {
		t.Fatalf("parse log entry: %v\nraw: %s", err, s)
	}
	return e
}
