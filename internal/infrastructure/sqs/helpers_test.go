package sqs

import (
	"testing"
	"time"
)

func TestSQSDelaySecondsCapsBeforeCast(t *testing.T) {
	until := time.Now().Add(2000 * time.Hour)
	got := sqsDelaySeconds(until)
	if got != 900 {
		t.Fatalf("sqsDelaySeconds = %d, want 900", got)
	}
}

func TestVisibilityTimeoutCapsBeforeCast(t *testing.T) {
	until := time.Now().Add(2000 * time.Hour)
	got := visibilityTimeout(until)
	if got != 43200 {
		t.Fatalf("visibilityTimeout = %d, want 43200", got)
	}
}
