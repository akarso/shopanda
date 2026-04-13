package cache_test

import (
	"context"
	"strings"
	"testing"
	"time"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- mock cache ---

type mockCache struct {
	entries        map[string]bool
	deletedPrefix  string
	deleteByPfxErr error
}

func newMockCache() *mockCache {
	return &mockCache{entries: make(map[string]bool)}
}

func (m *mockCache) Get(_ string, _ any) (bool, error)            { return false, nil }
func (m *mockCache) Set(key string, _ any, _ time.Duration) error { m.entries[key] = true; return nil }
func (m *mockCache) Delete(key string) error                      { delete(m.entries, key); return nil }
func (m *mockCache) DeleteByPrefix(_ context.Context, prefix string) error {
	m.deletedPrefix = prefix
	if m.deleteByPfxErr != nil {
		return m.deleteByPfxErr
	}
	for k := range m.entries {
		if strings.HasPrefix(k, prefix) {
			delete(m.entries, k)
		}
	}
	return nil
}

// --- mock logger ---

type mockLogger struct{}

func (m *mockLogger) Info(_ string, _ map[string]interface{})           {}
func (m *mockLogger) Warn(_ string, _ map[string]interface{})           {}
func (m *mockLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// --- tests ---

func TestProductKeyPrefix(t *testing.T) {
	got := cacheApp.ProductKeyPrefix("p123")
	want := "product:p123:"
	if got != want {
		t.Errorf("ProductKeyPrefix = %q, want %q", got, want)
	}
}

func TestInvalidation_ProductUpdated(t *testing.T) {
	mc := newMockCache()
	mc.entries["product:p1:store1:en:EUR"] = true
	mc.entries["product:p1:store2:de:EUR"] = true
	mc.entries["product:p2:store1:en:EUR"] = true

	log := &mockLogger{}
	sub := cacheApp.NewInvalidationSubscriber(mc, log)

	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{
		ProductID: "p1",
		Name:      "Widget",
		Slug:      "widget",
	})

	// OnAsync handlers run in goroutines; call handler directly for determinism.
	err := sub.HandleProductUpdated(context.Background(), evt)
	if err != nil {
		t.Fatalf("HandleProductUpdated: %v", err)
	}

	if mc.deletedPrefix != "product:p1:" {
		t.Errorf("deletedPrefix = %q, want %q", mc.deletedPrefix, "product:p1:")
	}
	// p1 entries deleted, p2 remains.
	if mc.entries["product:p1:store1:en:EUR"] {
		t.Error("product:p1:store1:en:EUR should be deleted")
	}
	if !mc.entries["product:p2:store1:en:EUR"] {
		t.Error("product:p2:store1:en:EUR should remain")
	}
}

func TestInvalidation_PriceUpserted(t *testing.T) {
	mc := newMockCache()
	mc.entries["product:p1:store1:en:EUR"] = true
	log := &mockLogger{}
	sub := cacheApp.NewInvalidationSubscriber(mc, log)

	evt := event.New(pricing.EventPriceUpserted, "test", pricing.PriceUpsertedData{
		PriceID:   "pr1",
		VariantID: "v1",
		ProductID: "p1",
		StoreID:   "store1",
		Currency:  "EUR",
		Amount:    1999,
	})

	err := sub.HandlePriceUpserted(context.Background(), evt)
	if err != nil {
		t.Fatalf("HandlePriceUpserted: %v", err)
	}

	if mc.deletedPrefix != "product:p1:" {
		t.Errorf("deletedPrefix = %q, want %q", mc.deletedPrefix, "product:p1:")
	}
	if mc.entries["product:p1:store1:en:EUR"] {
		t.Error("product:p1:store1:en:EUR should be deleted")
	}
}

func TestInvalidation_WrongEventData(t *testing.T) {
	mc := newMockCache()
	log := &mockLogger{}
	sub := cacheApp.NewInvalidationSubscriber(mc, log)

	evt := event.New(catalog.EventProductUpdated, "test", "wrong-type")
	err := sub.HandleProductUpdated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for wrong event data type")
	}
}

func TestInvalidation_WrongEventData_Price(t *testing.T) {
	mc := newMockCache()
	log := &mockLogger{}
	sub := cacheApp.NewInvalidationSubscriber(mc, log)

	evt := event.New(pricing.EventPriceUpserted, "test", "wrong-type")
	err := sub.HandlePriceUpserted(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for wrong event data type")
	}
}

func TestProductKeyPrefix_RejectsMetacharacters(t *testing.T) {
	for _, id := range []string{"a%b", "a_b"} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("ProductKeyPrefix(%q) should panic", id)
				}
			}()
			cacheApp.ProductKeyPrefix(id)
		}()
	}
}

func TestInvalidation_DeleteByPrefixError(t *testing.T) {
	mc := newMockCache()
	mc.deleteByPfxErr = context.DeadlineExceeded
	log := &mockLogger{}
	sub := cacheApp.NewInvalidationSubscriber(mc, log)

	evt := event.New(catalog.EventProductUpdated, "test", catalog.ProductUpdatedData{
		ProductID: "p1",
	})
	err := sub.HandleProductUpdated(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error when DeleteByPrefix fails")
	}
}
