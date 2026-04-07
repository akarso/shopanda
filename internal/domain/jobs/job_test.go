package jobs_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

func TestNewJob(t *testing.T) {
	j, err := jobs.NewJob("j1", "send_email", map[string]interface{}{"to": "a@b.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if j.ID != "j1" {
		t.Errorf("ID = %q, want %q", j.ID, "j1")
	}
	if j.Type != "send_email" {
		t.Errorf("Type = %q, want %q", j.Type, "send_email")
	}
	if j.Status != jobs.StatusPending {
		t.Errorf("Status = %q, want %q", j.Status, jobs.StatusPending)
	}
	if j.MaxRetries != jobs.DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", j.MaxRetries, jobs.DefaultMaxRetries)
	}
	if j.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", j.Attempts)
	}
	if j.Payload["to"] != "a@b.com" {
		t.Errorf("Payload[to] = %v, want a@b.com", j.Payload["to"])
	}
}

func TestNewJob_EmptyID(t *testing.T) {
	_, err := jobs.NewJob("", "send_email", nil)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestNewJob_EmptyType(t *testing.T) {
	_, err := jobs.NewJob("j1", "", nil)
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestNewJob_NilPayload(t *testing.T) {
	j, err := jobs.NewJob("j1", "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if j.Payload == nil {
		t.Fatal("expected non-nil payload map")
	}
}

func TestStatus_Constants(t *testing.T) {
	tests := []struct {
		status jobs.Status
		want   string
	}{
		{jobs.StatusPending, "pending"},
		{jobs.StatusProcessing, "processing"},
		{jobs.StatusDone, "done"},
		{jobs.StatusFailed, "failed"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("Status = %q, want %q", tt.status, tt.want)
		}
	}
}
