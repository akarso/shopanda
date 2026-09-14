// Package tagtest is the shared SetWithTags / DeleteByTag contract suite.
// Both Cache backends (and the in-memory stub) run Run so they stay at parity.
package tagtest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
)

// Run is the shared SetWithTags / DeleteByTag contract. Postgres and Redis
// backends both execute it so the two implementations stay at parity.
func Run(t *testing.T, c cache.Cache) {
	t.Helper()
	if c == nil {
		t.Fatal("nil cache")
	}
	ctx := context.Background()

	t.Run("SetWithTagsAndGet", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:page:a", "html-a", time.Hour, "cms:7", "cat:12"); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		assertHit(t, c, "tagtest:page:a", "html-a")
	})

	t.Run("SetWithTagsNoTagsStillStores", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:untagged", "plain", time.Hour); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		assertHit(t, c, "tagtest:untagged", "plain")
	})

	t.Run("DeleteByOneTagLeavesOtherTags", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:page:a", "a", time.Hour, "cms:7", "cat:12"); err != nil {
			t.Fatalf("SetWithTags a: %v", err)
		}
		if err := c.SetWithTags(ctx, "tagtest:page:b", "b", time.Hour, "cms:7"); err != nil {
			t.Fatalf("SetWithTags b: %v", err)
		}
		if err := c.SetWithTags(ctx, "tagtest:page:c", "c", time.Hour, "cat:12"); err != nil {
			t.Fatalf("SetWithTags c: %v", err)
		}
		if _, err := c.DeleteByTag(ctx, "cms:7"); err != nil {
			t.Fatalf("DeleteByTag: %v", err)
		}
		assertMiss(t, c, "tagtest:page:a")
		assertMiss(t, c, "tagtest:page:b")
		assertHit(t, c, "tagtest:page:c", "c")
	})

	t.Run("AdditiveAcrossSeparateSetWithTags", func(t *testing.T) {
		const key = "tagtest:additive"
		if err := c.SetWithTags(ctx, key, "v1", time.Hour, "add-a"); err != nil {
			t.Fatalf("SetWithTags A: %v", err)
		}
		if err := c.SetWithTags(ctx, key, "v2", time.Hour, "add-b"); err != nil {
			t.Fatalf("SetWithTags B: %v", err)
		}
		assertHit(t, c, key, "v2")
		if _, err := c.DeleteByTag(ctx, "add-a"); err != nil {
			t.Fatalf("DeleteByTag A: %v", err)
		}
		assertMiss(t, c, key)
	})

	t.Run("DeleteByTagCountIsSnapshotSize", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:count:a", "a", time.Hour, "count-tag"); err != nil {
			t.Fatalf("SetWithTags a: %v", err)
		}
		if err := c.SetWithTags(ctx, "tagtest:count:b", "b", time.Hour, "count-tag"); err != nil {
			t.Fatalf("SetWithTags b: %v", err)
		}
		n, err := c.DeleteByTag(ctx, "count-tag")
		if err != nil {
			t.Fatalf("DeleteByTag: %v", err)
		}
		if n != 2 {
			t.Fatalf("DeleteByTag count = %d, want 2 (snapshot of both live keys)", n)
		}
		assertMiss(t, c, "tagtest:count:a")
		assertMiss(t, c, "tagtest:count:b")
	})

	t.Run("DeleteByTagMissingIsNoop", func(t *testing.T) {
		n, err := c.DeleteByTag(ctx, "tagtest:never-used")
		if err != nil {
			t.Fatalf("DeleteByTag missing: %v", err)
		}
		if n != 0 {
			t.Fatalf("DeleteByTag missing count = %d, want 0", n)
		}
	})

	t.Run("DeleteByTagEmptyIsNoop", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:keep", "v", time.Hour, "keep"); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		if _, err := c.DeleteByTag(ctx, ""); err != nil {
			t.Fatalf("DeleteByTag empty: %v", err)
		}
		if _, err := c.DeleteByTag(ctx, "  "); err != nil {
			t.Fatalf("DeleteByTag whitespace: %v", err)
		}
		assertHit(t, c, "tagtest:keep", "v")
	})

	t.Run("DeleteByTagTrimsLikeSetWithTags", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:padded", "v", time.Hour, "  pad-tag  "); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		n, err := c.DeleteByTag(ctx, "  pad-tag  ")
		if err != nil {
			t.Fatalf("DeleteByTag padded: %v", err)
		}
		if n != 1 {
			t.Fatalf("DeleteByTag padded count = %d, want 1", n)
		}
		assertMiss(t, c, "tagtest:padded")

		if err := c.SetWithTags(ctx, "tagtest:unpadded", "v", time.Hour, "pad-tag"); err != nil {
			t.Fatalf("SetWithTags unpadded: %v", err)
		}
		n, err = c.DeleteByTag(ctx, "  pad-tag  ")
		if err != nil {
			t.Fatalf("DeleteByTag trim of unpadded write: %v", err)
		}
		if n != 1 {
			t.Fatalf("DeleteByTag trim count = %d, want 1", n)
		}
		assertMiss(t, c, "tagtest:unpadded")
	})

	t.Run("SetWithTagsHonorsCancelledContext", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.SetWithTags(cancelled, "tagtest:cancelled-empty", "v", time.Hour); err == nil {
			t.Fatal("SetWithTags with cancelled ctx (no tags) expected error")
		}
		if err := c.SetWithTags(cancelled, "tagtest:cancelled-ws", "v", time.Hour, "  "); err == nil {
			t.Fatal("SetWithTags with cancelled ctx (whitespace tags) expected error")
		}
		assertMiss(t, c, "tagtest:cancelled-empty")
		assertMiss(t, c, "tagtest:cancelled-ws")
	})

	t.Run("DeleteByTagAfterDeleteIsNoop", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:gone", "v", time.Hour, "gone-tag"); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		if err := c.Delete("tagtest:gone"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := c.DeleteByTag(ctx, "gone-tag"); err != nil {
			t.Fatalf("DeleteByTag after Delete: %v", err)
		}
		assertMiss(t, c, "tagtest:gone")
	})

	t.Run("DuplicateTagsAreIdempotent", func(t *testing.T) {
		if err := c.SetWithTags(ctx, "tagtest:dup", "v", time.Hour, "dup-tag", "dup-tag", ""); err != nil {
			t.Fatalf("SetWithTags: %v", err)
		}
		if _, err := c.DeleteByTag(ctx, "dup-tag"); err != nil {
			t.Fatalf("DeleteByTag: %v", err)
		}
		assertMiss(t, c, "tagtest:dup")
	})

	t.Run("ConcurrentSetWithTagsKeepsMembership", func(t *testing.T) {
		// A concurrent SetWithTags that commits after DeleteByTag has
		// snapshotted membership must still be reachable by a later
		// DeleteByTag — the original two-step delete dropped that
		// association and left the value permanently un-invalidatable.
		for i := 0; i < 20; i++ {
			tag := fmt.Sprintf("tagtest:race:%d", i)
			newKey := fmt.Sprintf("tagtest:race:new:%d", i)
			for j := 0; j < 32; j++ {
				oldKey := fmt.Sprintf("tagtest:race:old:%d:%d", i, j)
				if err := c.SetWithTags(ctx, oldKey, "old", time.Hour, tag); err != nil {
					t.Fatalf("seed %s: %v", oldKey, err)
				}
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, _ = c.DeleteByTag(ctx, tag)
			}()
			go func() {
				defer wg.Done()
				_ = c.SetWithTags(ctx, newKey, "fresh", time.Hour, tag)
			}()
			wg.Wait()
			if _, err := c.DeleteByTag(ctx, tag); err != nil {
				t.Fatalf("follow-up DeleteByTag: %v", err)
			}
			assertMiss(t, c, newKey)
		}
	})
}

func assertHit(t *testing.T, c cache.Cache, key, want string) {
	t.Helper()
	var got string
	hit, err := c.Get(key, &got)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	if !hit {
		t.Fatalf("Get(%q): miss, want %q", key, want)
	}
	if got != want {
		t.Fatalf("Get(%q) = %q, want %q", key, got, want)
	}
}

func assertMiss(t *testing.T, c cache.Cache, key string) {
	t.Helper()
	var got string
	hit, err := c.Get(key, &got)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	if hit {
		t.Fatalf("Get(%q): hit %q, want miss", key, got)
	}
}
