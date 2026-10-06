package postgres

import "time"

// SetStatsCountTimeout sets this store's COUNT(*) cap so a test can
// force the pg_class.reltuples fallback without waiting 1.5s and
// without mutating other stores.
func SetStatsCountTimeout(s *CacheStore, d time.Duration) {
	s.statsCountTimeout = new(time.Duration)
	*s.statsCountTimeout = d
}
