package redis

func SetAfterTagRename(s *CacheStore, fn func()) {
	s.afterTagRename = fn
}

// SetDeleteByPrefixBatchSize overrides the shared flush-batch threshold
// for a test, returning a restore func the test should defer.
func SetDeleteByPrefixBatchSize(n int) (restore func()) {
	prev := deleteByPrefixBatchSize
	deleteByPrefixBatchSize = n
	return func() { deleteByPrefixBatchSize = prev }
}

func SetAfterPrefixBatchFlushed(s *CacheStore, fn func()) {
	s.afterPrefixBatchFlushed = fn
}

func SetAfterTagMembersFlushed(s *CacheStore, fn func()) {
	s.afterTagMembersFlushed = fn
}
