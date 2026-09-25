package redis

import "time"

func SetAfterTagRename(s *CacheStore, fn func()) {
	s.afterTagRename = fn
}

// SetRateLimiterClock overrides RateLimiter's clock for a test. Nil
// restores time.Now.
func SetRateLimiterClock(l *RateLimiter, now func() time.Time) {
	l.now = now
}
