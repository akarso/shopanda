package cache_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cacheapp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/cache"
)

type fakeAdminCache struct {
	keys      map[string]string
	tags      map[string]map[string]struct{}
	stats     cache.Stats
	statsErr  error
	flushN    int64
	flushErr  error
	prefixErr error
	deleteErr error
	tagErr    error
}

func newFakeAdminCache() *fakeAdminCache {
	return &fakeAdminCache{
		keys:  make(map[string]string),
		tags:  make(map[string]map[string]struct{}),
		stats: cache.Stats{Backend: "fake", Keys: 0},
	}
}

func (f *fakeAdminCache) Get(string, any) (bool, error)                    { return false, nil }
func (f *fakeAdminCache) Set(string, any, time.Duration) error             { return nil }
func (f *fakeAdminCache) Incr(string, int64, time.Duration) (int64, error) { return 0, nil }
func (f *fakeAdminCache) CompareAndSubtract(string, int64) (int64, error)  { return 0, nil }

func (f *fakeAdminCache) Delete(key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.keys, key)
	return nil
}

func (f *fakeAdminCache) DeleteByPrefix(_ context.Context, prefix string) error {
	if f.prefixErr != nil {
		return f.prefixErr
	}
	for k := range f.keys {
		if strings.HasPrefix(k, prefix) {
			delete(f.keys, k)
		}
	}
	return nil
}

func (f *fakeAdminCache) SetWithTags(_ context.Context, key string, value any, _ time.Duration, tags ...string) error {
	f.keys[key] = "v"
	_ = value
	for _, tag := range tags {
		if f.tags[tag] == nil {
			f.tags[tag] = make(map[string]struct{})
		}
		f.tags[tag][key] = struct{}{}
	}
	return nil
}

func (f *fakeAdminCache) DeleteByTag(_ context.Context, tag string) (int64, error) {
	if f.tagErr != nil {
		return 0, f.tagErr
	}
	keys := f.tags[tag]
	delete(f.tags, tag)
	var n int64
	for k := range keys {
		delete(f.keys, k)
		n++
	}
	return n, nil
}

func (f *fakeAdminCache) Stats(context.Context) (cache.Stats, error) {
	if f.statsErr != nil {
		return cache.Stats{}, f.statsErr
	}
	st := f.stats
	st.Keys = int64(len(f.keys))
	return st, nil
}

func (f *fakeAdminCache) FlushAll(context.Context) (int64, error) {
	if f.flushErr != nil {
		return 0, f.flushErr
	}
	n := int64(len(f.keys))
	f.keys = make(map[string]string)
	f.tags = make(map[string]map[string]struct{})
	if f.flushN != 0 {
		n = f.flushN
	}
	return n, nil
}

var _ cache.Cache = (*fakeAdminCache)(nil)

func TestAdminService_StatsIncludesL1(t *testing.T) {
	backend := newFakeAdminCache()
	_ = backend.SetWithTags(context.Background(), "a", "1", 0)
	cleared := false
	svc := cacheapp.NewAdminService(backend, []cacheapp.L1Source{{
		Name: "rbac.catalog",
		Snap: func() (int, int64, int64) { return 1, 10, 2 },
		Clear: func() {
			cleared = true
		},
	}})
	snap, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if snap.L2.Backend != "fake" || snap.L2.Keys != 1 {
		t.Fatalf("l2 = %+v", snap.L2)
	}
	if len(snap.L1) != 1 || snap.L1[0].Name != "rbac.catalog" || snap.L1[0].Hits != 10 {
		t.Fatalf("l1 = %+v", snap.L1)
	}
	if cleared {
		t.Fatal("Stats must not clear L1")
	}
}

func TestAdminService_Clear_ExactlyOneSelector(t *testing.T) {
	svc := cacheapp.NewAdminService(newFakeAdminCache(), nil)
	_, err := svc.Clear(context.Background(), cacheapp.ClearRequest{})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want exactly-one validation", err)
	}
	_, err = svc.Clear(context.Background(), cacheapp.ClearRequest{Prefix: "p", All: true})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want exactly-one validation for prefix+all", err)
	}
	_, err = svc.Clear(context.Background(), cacheapp.ClearRequest{Prefix: "   "})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want whitespace prefix treated as omitted", err)
	}
}

func TestAdminService_Clear_PrefixTagKeyAll(t *testing.T) {
	ctx := context.Background()
	backend := newFakeAdminCache()
	_ = backend.SetWithTags(ctx, "product:1:en", "a", 0, "product:1")
	_ = backend.SetWithTags(ctx, "product:2:en", "b", 0, "product:2")
	_ = backend.SetWithTags(ctx, "other", "c", 0)

	l1Cleared := 0
	svc := cacheapp.NewAdminService(backend, []cacheapp.L1Source{{
		Name:  "rbac.catalog",
		Snap:  func() (int, int64, int64) { return 1, 0, 0 },
		Clear: func() { l1Cleared++ },
	}})

	res, err := svc.Clear(ctx, cacheapp.ClearRequest{Prefix: "product:1:"})
	if err != nil {
		t.Fatalf("prefix: %v", err)
	}
	if res.Mode != cacheapp.ClearPrefix || res.Target != "product:1:" {
		t.Fatalf("prefix result = %+v", res)
	}
	if _, ok := backend.keys["product:1:en"]; ok {
		t.Fatal("prefix clear left product:1:en")
	}
	if _, ok := backend.keys["product:2:en"]; !ok {
		t.Fatal("prefix clear removed product:2:en")
	}
	if l1Cleared != 0 {
		t.Fatal("targeted clear must not flush L1")
	}

	res, err = svc.Clear(ctx, cacheapp.ClearRequest{Tag: "product:2"})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if res.Mode != cacheapp.ClearTag || res.Deleted == nil || *res.Deleted != 1 {
		t.Fatalf("tag result = %+v", res)
	}

	res, err = svc.Clear(ctx, cacheapp.ClearRequest{Key: "other"})
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if res.Mode != cacheapp.ClearKey || res.Target != "other" {
		t.Fatalf("key result = %+v", res)
	}

	_ = backend.SetWithTags(ctx, "left", "z", 0)
	res, err = svc.Clear(ctx, cacheapp.ClearRequest{All: true})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if res.Mode != cacheapp.ClearAll || len(backend.keys) != 0 {
		t.Fatalf("all result = %+v keys=%v", res, backend.keys)
	}
	if res.Deleted == nil || *res.Deleted != 1 {
		t.Fatalf("all deleted = %v, want 1", res.Deleted)
	}
	if l1Cleared != 1 {
		t.Fatalf("all must clear L1, got %d", l1Cleared)
	}

	res, err = svc.Clear(ctx, cacheapp.ClearRequest{All: true})
	if err != nil {
		t.Fatalf("empty all: %v", err)
	}
	if res.Deleted == nil || *res.Deleted != 0 {
		t.Fatalf("empty all deleted = %v, want 0", res.Deleted)
	}
}

func TestAdminService_Clear_BackendErrorKeepsSelector(t *testing.T) {
	backend := newFakeAdminCache()
	backend.prefixErr = errors.New("redis scan failed")
	svc := cacheapp.NewAdminService(backend, nil)
	res, err := svc.Clear(context.Background(), cacheapp.ClearRequest{Prefix: "product:1:"})
	if err == nil {
		t.Fatal("want backend error")
	}
	if res.Mode != cacheapp.ClearPrefix || res.Target != "product:1:" {
		t.Fatalf("error result = %+v, want mode=prefix target=product:1:", res)
	}

	backend.prefixErr = nil
	backend.flushErr = errors.New("flush failed")
	res, err = svc.Clear(context.Background(), cacheapp.ClearRequest{All: true})
	if err == nil {
		t.Fatal("want flush error")
	}
	if res.Mode != cacheapp.ClearAll {
		t.Fatalf("flush error result = %+v, want mode=all", res)
	}
	if res.Deleted != nil {
		t.Fatalf("flush error deleted = %v, want omitted when count is unknown", res.Deleted)
	}
}

func TestAdminService_Clear_PreservesKeyAndPrefixWhitespace(t *testing.T) {
	ctx := context.Background()
	backend := newFakeAdminCache()
	_ = backend.SetWithTags(ctx, " item ", "spaced", 0)
	_ = backend.SetWithTags(ctx, "item", "trimmed", 0)
	_ = backend.SetWithTags(ctx, "p: keep", "a", 0)
	_ = backend.SetWithTags(ctx, "p:x", "b", 0)

	svc := cacheapp.NewAdminService(backend, nil)
	res, err := svc.Clear(ctx, cacheapp.ClearRequest{Key: " item "})
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if res.Target != " item " {
		t.Fatalf("target = %q, want literal padded key", res.Target)
	}
	if _, ok := backend.keys[" item "]; ok {
		t.Fatal("padded key should be deleted")
	}
	if _, ok := backend.keys["item"]; !ok {
		t.Fatal("unpadded key must survive")
	}

	res, err = svc.Clear(ctx, cacheapp.ClearRequest{Prefix: "p: "})
	if err != nil {
		t.Fatalf("prefix: %v", err)
	}
	if res.Target != "p: " {
		t.Fatalf("prefix target = %q, want literal padded prefix", res.Target)
	}
	if _, ok := backend.keys["p: keep"]; ok {
		t.Fatal("prefix \"p: \" should delete p: keep")
	}
	if _, ok := backend.keys["p:x"]; !ok {
		t.Fatal("prefix \"p: \" must not delete p:x")
	}
}
