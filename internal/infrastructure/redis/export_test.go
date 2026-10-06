package redis

func SetAfterTagRename(s *CacheStore, fn func()) {
	s.afterTagRename = fn
}

func SetBeforeFlushDel(s *CacheStore, fn func()) {
	s.beforeFlushDel = fn
}
