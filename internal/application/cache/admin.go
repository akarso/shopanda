package cache

import (
	"context"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// L1Snapshot is one process-local cache's occupancy and hit/miss counters
// (PR-1042). Hits/misses are lifetime for this process, not reset by Clear.
type L1Snapshot struct {
	Name    string `json:"name"`
	Entries int    `json:"entries"`
	Hits    int64  `json:"hits"`
	Misses  int64  `json:"misses"`
}

// L1Source is one named L1 store the admin surface can report on and
// flush. Snap/Clear are called from Stats/Clear; they must be safe for
// concurrent use with the store's ordinary readers.
type L1Source struct {
	Name  string
	Snap  func() (entries int, hits, misses int64)
	Clear func()
}

// Snapshot is the GET /admin/cache/stats payload: L2 backend occupancy
// plus every registered L1 store in this process.
type Snapshot struct {
	L2 cache.Stats  `json:"l2"`
	L1 []L1Snapshot `json:"l1"`
}

// ClearMode names which selector a Clear call used.
type ClearMode string

const (
	ClearPrefix ClearMode = "prefix"
	ClearTag    ClearMode = "tag"
	ClearKey    ClearMode = "key"
	ClearAll    ClearMode = "all"
)

// ClearRequest is exactly one of Prefix, Tag, Key, or All. Whitespace-only
// selectors are treated as omitted so `{"prefix":"  "}` cannot sneak a
// full-table DeleteByPrefix past the distinct cache.clear_all permission.
// Non-blank prefix and key are passed through unchanged — backends store
// keys literally, including leading/trailing spaces. Tags are trimmed to
// match NormalizeTag / SetWithTags.
type ClearRequest struct {
	Prefix string
	Tag    string
	Key    string
	All    bool
}

// ClearResult is the POST /admin/cache/clear payload.
type ClearResult struct {
	Mode    ClearMode `json:"mode"`
	Target  string    `json:"target,omitempty"`
	Deleted *int64    `json:"deleted,omitempty"`
}

// AdminService is the shared implementation HTTP and CLI cache admin
// call (PR-1042) — no separate logic path, matching jobs admin.
type AdminService struct {
	backend cache.Cache
	mu      sync.Mutex
	l1      []L1Source
}

// NewAdminService constructs an AdminService over backend. backend must
// not be nil. l1 may be nil; RegisterL1 can add stores later (storefront
// L1 is constructed after the HTTP runtime in this repo).
func NewAdminService(backend cache.Cache, l1 []L1Source) *AdminService {
	if backend == nil {
		panic("cache.NewAdminService: nil cache")
	}
	out := make([]L1Source, len(l1))
	copy(out, l1)
	return &AdminService{backend: backend, l1: out}
}

// RegisterL1 appends stores that were not available at construction
// (the storefront category-nav L1 is created only when frontend.enabled).
func (s *AdminService) RegisterL1(stores ...L1Source) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.l1 = append(s.l1, stores...)
}

// Stats returns L2 occupancy plus a snapshot of every registered L1 store.
func (s *AdminService) Stats(ctx context.Context) (Snapshot, error) {
	l2, err := s.backend.Stats(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	sources := append([]L1Source(nil), s.l1...)
	s.mu.Unlock()

	l1 := make([]L1Snapshot, 0, len(sources))
	for _, src := range sources {
		snap := L1Snapshot{Name: src.Name}
		if src.Snap != nil {
			snap.Entries, snap.Hits, snap.Misses = src.Snap()
		}
		l1 = append(l1, snap)
	}
	return Snapshot{L2: l2, L1: l1}, nil
}

// Clear applies exactly one selector against L2. All also clears every
// registered L1 store in this process (other replicas keep their L1 until
// TTL). Targeted prefix/tag/key clears do not touch L1 — those stores
// are not keyed the same way as L2.
func (s *AdminService) Clear(ctx context.Context, req ClearRequest) (ClearResult, error) {
	mode, target, err := req.Normalize()
	if err != nil {
		return ClearResult{}, err
	}
	switch mode {
	case ClearPrefix:
		if err := s.backend.DeleteByPrefix(ctx, target); err != nil {
			return ClearResult{Mode: mode, Target: target}, err
		}
		return ClearResult{Mode: mode, Target: target}, nil
	case ClearTag:
		n, err := s.backend.DeleteByTag(ctx, target)
		if err != nil {
			return ClearResult{Mode: mode, Target: target}, err
		}
		return ClearResult{Mode: mode, Target: target, Deleted: int64Ptr(n)}, nil
	case ClearKey:
		if err := s.backend.Delete(target); err != nil {
			return ClearResult{Mode: mode, Target: target}, err
		}
		return ClearResult{Mode: mode, Target: target}, nil
	case ClearAll:
		n, err := s.backend.FlushAll(ctx)
		res := ClearResult{Mode: mode}
		if n != 0 {
			res.Deleted = int64Ptr(n)
		}
		if err != nil {
			return res, err
		}
		s.clearL1()
		return res, nil
	default:
		return ClearResult{}, apperror.Validation("exactly one of prefix, tag, key, or all is required")
	}
}

func (s *AdminService) clearL1() {
	s.mu.Lock()
	sources := append([]L1Source(nil), s.l1...)
	s.mu.Unlock()
	for _, src := range sources {
		if src.Clear != nil {
			src.Clear()
		}
	}
}

func (req ClearRequest) Normalize() (ClearMode, string, error) {
	prefixOK := strings.TrimSpace(req.Prefix) != ""
	tag := cache.NormalizeTag(req.Tag)
	keyOK := strings.TrimSpace(req.Key) != ""

	n := 0
	var mode ClearMode
	var target string
	if prefixOK {
		n++
		mode, target = ClearPrefix, req.Prefix
	}
	if tag != "" {
		n++
		mode, target = ClearTag, tag
	}
	if keyOK {
		n++
		mode, target = ClearKey, req.Key
	}
	if req.All {
		n++
		mode, target = ClearAll, ""
	}
	if n != 1 {
		return "", "", apperror.Validation("exactly one of prefix, tag, key, or all is required")
	}
	return mode, target, nil
}

func int64Ptr(n int64) *int64 { return &n }
