package postgres

import (
	"testing"
	"time"
)

func TestRetryDelay_ExponentialGrowth(t *testing.T) {
	// Each attempt should roughly double the previous delay.
	prev := time.Duration(0)
	for attempt := 0; attempt < 5; attempt++ {
		d := retryDelay(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: delay must be positive, got %v", attempt, d)
		}
		if attempt > 0 && d <= prev/2 {
			t.Errorf("attempt %d: delay %v should be roughly double previous %v", attempt, d, prev)
		}
		prev = d
	}
}

func TestRetryDelay_CappedAtMax(t *testing.T) {
	// Very high attempt number should not exceed backoffMax + jitter.
	maxWithJitter := time.Duration(float64(backoffMax) * (1 + jitterRatio))
	for attempt := 10; attempt < 15; attempt++ {
		d := retryDelay(attempt)
		if d > maxWithJitter {
			t.Errorf("attempt %d: delay %v exceeds cap %v", attempt, d, maxWithJitter)
		}
	}
}

func TestRetryDelay_FirstAttemptNearBase(t *testing.T) {
	// attempt=0 should produce ~backoffBase ±25%.
	lo := time.Duration(float64(backoffBase) * (1 - jitterRatio))
	hi := time.Duration(float64(backoffBase) * (1 + jitterRatio))

	for i := 0; i < 50; i++ {
		d := retryDelay(0)
		if d < lo || d > hi {
			t.Fatalf("attempt 0: delay %v outside [%v, %v]", d, lo, hi)
		}
	}
}
