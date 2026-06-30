package jobqueue_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/jobqueue"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	msg := jobqueue.Message{
		ID:         "job-1",
		Type:       "email.send",
		Payload:    map[string]interface{}{"to": "a@example.com"},
		Status:     jobs.StatusPending,
		Attempts:   0,
		MaxRetries: 3,
		RunAt:      now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	data, err := jobqueue.Encode(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := jobqueue.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != msg.ID || got.Type != msg.Type || got.Status != msg.Status {
		t.Fatalf("round trip = %+v, want %+v", got, msg)
	}
}

func TestFromJobDefaultsPayload(t *testing.T) {
	job, err := jobs.NewJob("job-1", "test", nil)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	msg := jobqueue.FromJob(job)
	if msg.Payload == nil {
		t.Fatal("expected non-nil payload map")
	}
}
