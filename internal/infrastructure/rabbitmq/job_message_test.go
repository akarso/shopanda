package rabbitmq_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	inrabbitmq "github.com/akarso/shopanda/internal/infrastructure/rabbitmq"
)

func TestEncodeDecodeJobMessage_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	job := jobs.Job{
		ID:         "job-1",
		Type:       "email.send",
		Payload:    map[string]interface{}{"to": "a@b.com"},
		Status:     jobs.StatusPending,
		Attempts:   0,
		MaxRetries: 3,
		RunAt:      now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := inrabbitmq.EncodeJobMessageForTest(job)
	if err != nil {
		t.Fatalf("EncodeJobMessageForTest: %v", err)
	}
	got, err := inrabbitmq.DecodeJobMessageForTest(data)
	if err != nil {
		t.Fatalf("DecodeJobMessageForTest: %v", err)
	}
	if got.ID != job.ID || got.Type != job.Type || got.Status != job.Status {
		t.Fatalf("round trip = %+v, want %+v", got, job)
	}
	if got.Payload["to"] != "a@b.com" {
		t.Fatalf("payload = %v", got.Payload)
	}
}

func TestNewJobQueue_EmptyURL(t *testing.T) {
	if _, err := inrabbitmq.NewJobQueue(inrabbitmq.QueueConfig{}); err == nil {
		t.Fatal("NewJobQueue() expected error for empty url")
	}
}
