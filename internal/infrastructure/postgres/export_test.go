package postgres

import "time"

// SetStatsCountTimeout overrides the COUNT(*) cap used by Stats so tests
// can force the pg_class.reltuples fallback without waiting 1.5s.
func SetStatsCountTimeout(d time.Duration) (restore func()) {
	old := statsCountTimeout
	statsCountTimeout = d
	return func() { statsCountTimeout = old }
}
