package integrationhttp

import (
	"context"
	"sync"
	"time"
)

// ReplayStore tracks seen nonce/signature pairs for HMAC replay protection.
type ReplayStore interface {
	Seen(ctx context.Context, namespace, nonce, signature string, now, expiresAt time.Time) (bool, error)
	Remember(ctx context.Context, namespace, nonce, signature string, expiresAt time.Time) error
}

// MemoryReplayStore is an in-process replay store suitable for tests and single-node dev.
type MemoryReplayStore struct {
	mu    sync.Mutex
	items map[string]time.Time
}

// NewMemoryReplayStore creates an empty in-memory replay store.
func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{items: make(map[string]time.Time)}
}

func (s *MemoryReplayStore) Seen(_ context.Context, namespace, nonce, signature string, now, expiresAt time.Time) (bool, error) {
	key := replayKey(namespace, nonce, signature)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if exp, ok := s.items[key]; ok && exp.After(now) {
		return true, nil
	}
	return false, nil
}

func (s *MemoryReplayStore) Remember(_ context.Context, namespace, nonce, signature string, expiresAt time.Time) error {
	key := replayKey(namespace, nonce, signature)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = expiresAt
	return nil
}

func (s *MemoryReplayStore) pruneLocked(now time.Time) {
	for key, exp := range s.items {
		if !exp.After(now) {
			delete(s.items, key)
		}
	}
}

func replayKey(namespace, nonce, signature string) string {
	return namespace + "\x00" + nonce + "\x00" + signature
}
