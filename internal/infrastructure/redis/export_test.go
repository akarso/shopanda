package redis

func SetAfterTagRename(s *CacheStore, fn func()) {
	s.afterTagRename = fn
}
