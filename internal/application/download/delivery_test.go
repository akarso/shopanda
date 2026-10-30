package download_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestService_PrepareDelivery_OwnershipAndGates(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	expires := now.Add(time.Hour)

	owned, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	got, err := svc.PrepareDelivery(context.Background(), "cust-1", owned.ID)
	if err != nil || got.ID != owned.ID {
		t.Fatalf("PrepareDelivery owner: %+v %v", got, err)
	}
	if got.DownloadCount != 0 {
		t.Fatalf("PrepareDelivery must not increment count, got %d", got.DownloadCount)
	}

	_, err = svc.PrepareDelivery(context.Background(), "other", owned.ID)
	if err == nil || !apperror.Is(err, apperror.CodeForbidden) {
		t.Fatalf("wrong owner: %v", err)
	}

	_, err = svc.PrepareDelivery(context.Background(), "", owned.ID)
	if err == nil || !apperror.Is(err, apperror.CodeUnauthorized) {
		t.Fatalf("empty customer: %v", err)
	}

	guest, err := domain.NewDownloadGrant(id.New(), domain.OrderItemID(id.New(), "vg"), "", "asset-g", expires, 1)
	if err != nil {
		t.Fatalf("NewDownloadGrant guest: %v", err)
	}
	if err := repo.Insert(context.Background(), guest); err != nil {
		t.Fatalf("Insert guest: %v", err)
	}
	_, err = svc.PrepareDelivery(context.Background(), "cust-1", guest.ID)
	if err == nil || !apperror.Is(err, apperror.CodeForbidden) {
		t.Fatalf("guest grant: %v", err)
	}

	expired, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "ve"), "cust-1", "asset-e", expires, 1)
	if err != nil {
		t.Fatalf("Issue expired: %v", err)
	}
	expired.ExpiresAt = now.Add(-time.Second)
	_ = repo.Update(context.Background(), expired)
	_, err = svc.PrepareDelivery(context.Background(), "cust-1", expired.ID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("expired: %v", err)
	}

	exhausted, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "vx"), "cust-1", "asset-x", expires, 1)
	if err != nil {
		t.Fatalf("Issue exhausted: %v", err)
	}
	if _, err := svc.ClaimDelivery(context.Background(), "cust-1", exhausted.ID); err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	_, err = svc.PrepareDelivery(context.Background(), "cust-1", exhausted.ID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !errors.Is(err, domain.ErrExhausted) {
		t.Fatalf("exhausted: %v", err)
	}
}

func TestService_ClaimDelivery_OwnershipRequired(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	expires := time.Now().UTC().Add(time.Hour)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", expires, 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = svc.ClaimDelivery(context.Background(), "other", g.ID)
	if err == nil || !apperror.Is(err, apperror.CodeForbidden) {
		t.Fatalf("wrong owner claim: %v", err)
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != 0 {
		t.Fatalf("count must stay 0 on forbidden claim, got %d", stored.DownloadCount)
	}

	got, err := svc.ClaimDelivery(context.Background(), "cust-1", g.ID)
	if err != nil || got.DownloadCount != 1 {
		t.Fatalf("ClaimDelivery: %+v %v", got, err)
	}
}

func TestService_ClaimDelivery_ConcurrentUpToLimit(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	limit := 8
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "cust-1", "asset-1", time.Now().UTC().Add(time.Hour), limit)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var ok atomic.Int64
	var wg sync.WaitGroup
	n := 40
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := svc.ClaimDelivery(context.Background(), "cust-1", g.ID); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := ok.Load(); got != int64(limit) {
		t.Fatalf("successful claims = %d, want %d", got, limit)
	}
	stored, _ := repo.FindByID(context.Background(), g.ID)
	if stored.DownloadCount != limit {
		t.Fatalf("download_count = %d, want %d", stored.DownloadCount, limit)
	}
}

func TestService_RecordDownload_RequiresLinkedCustomer(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	guest, err := domain.NewDownloadGrant(id.New(), domain.OrderItemID(id.New(), "vg"), "", "asset-g", time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(context.Background(), guest); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	_, err = svc.RecordDownload(context.Background(), guest.ID)
	if err == nil || !apperror.Is(err, apperror.CodeForbidden) {
		t.Fatalf("guest RecordDownload = %v", err)
	}
}
