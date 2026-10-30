package download_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewDownloadGrant_OK(t *testing.T) {
	expires := time.Now().UTC().Add(24 * time.Hour)
	g, err := download.NewDownloadGrant(id.New(), "ord/var", "cust-1", "asset-1", expires, 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if g.DownloadCount != 0 {
		t.Fatalf("DownloadCount = %d, want 0", g.DownloadCount)
	}
	if g.CustomerID != "cust-1" {
		t.Fatalf("CustomerID = %q", g.CustomerID)
	}
}

func TestNewDownloadGrant_TrimsAndRejects(t *testing.T) {
	expires := time.Now().UTC().Add(time.Hour)
	_, err := download.NewDownloadGrant("  ", "oi", "c", "a", expires, 1)
	if err == nil {
		t.Fatal("expected empty id error")
	}
	_, err = download.NewDownloadGrant(id.New(), "", "c", "a", expires, 1)
	if err == nil {
		t.Fatal("expected empty order item error")
	}
	_, err = download.NewDownloadGrant(id.New(), "oi", "c", "", expires, 1)
	if err == nil {
		t.Fatal("expected empty asset error")
	}
	_, err = download.NewDownloadGrant(id.New(), "oi", "c", "a", time.Time{}, 1)
	if err == nil {
		t.Fatal("expected zero expires error")
	}
	_, err = download.NewDownloadGrant(id.New(), "oi", "c", "a", expires, 0)
	if err == nil {
		t.Fatal("expected limit < 1 error")
	}
	_, err = download.NewDownloadGrant(id.New(), "oi", "c", "a", expires, download.MaxDownloadLimit+1)
	if err == nil {
		t.Fatal("expected limit too high error")
	}
}

func TestNewDownloadGrant_AllowsEmptyCustomer(t *testing.T) {
	expires := time.Now().UTC().Add(time.Hour)
	g, err := download.NewDownloadGrant(id.New(), "oi", "  ", "a", expires, 1)
	if err != nil {
		t.Fatalf("guest grant: %v", err)
	}
	if g.CustomerID != "" {
		t.Fatalf("CustomerID = %q, want empty", g.CustomerID)
	}
}

func TestCanDownload_AndRecord(t *testing.T) {
	now := time.Now().UTC()
	g, err := download.NewDownloadGrant(id.New(), "oi", "c", "a", now.Add(time.Hour), 2)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := g.CanDownload(now); err != nil {
		t.Fatalf("CanDownload: %v", err)
	}
	if err := g.RecordDownload(now); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	if g.DownloadCount != 1 {
		t.Fatalf("count = %d", g.DownloadCount)
	}
	if err := g.RecordDownload(now); err != nil {
		t.Fatalf("second RecordDownload: %v", err)
	}
	if err := g.CanDownload(now); err == nil || !errors.Is(err, download.ErrExhausted) {
		t.Fatalf("CanDownload after exhaust = %v", err)
	}
}

func TestCanDownload_Expired(t *testing.T) {
	now := time.Now().UTC()
	g, err := download.NewDownloadGrant(id.New(), "oi", "c", "a", now.Add(-time.Second), 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := g.CanDownload(now); err == nil || !errors.Is(err, download.ErrExpired) {
		t.Fatalf("CanDownload = %v", err)
	}
	if err := g.RecordDownload(now); err == nil || !errors.Is(err, download.ErrExpired) {
		t.Fatalf("RecordDownload on expired = %v", err)
	}
}

func TestRevoke_Idempotent(t *testing.T) {
	now := time.Now().UTC()
	g, err := download.NewDownloadGrant(id.New(), "oi", "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := g.Revoke(now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !g.Expired(now) {
		t.Fatal("expected expired after revoke")
	}
	if err := g.Revoke(now); err != nil {
		t.Fatalf("second Revoke: %v", err)
	}
}

func TestExtend_AndResetCount(t *testing.T) {
	now := time.Now().UTC()
	g, err := download.NewDownloadGrant(id.New(), "oi", "c", "a", now.Add(time.Hour), 2)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	_ = g.RecordDownload(now)
	later := now.Add(48 * time.Hour)
	if err := g.Extend(later); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if !g.ExpiresAt.Equal(later) {
		t.Fatalf("ExpiresAt = %v", g.ExpiresAt)
	}
	if err := g.ResetCount(); err != nil {
		t.Fatalf("ResetCount: %v", err)
	}
	if g.DownloadCount != 0 {
		t.Fatalf("count = %d", g.DownloadCount)
	}
	if err := g.Extend(g.CreatedAt); err == nil {
		t.Fatal("Extend to created_at must fail")
	}
}

func TestOrderItemID_RoundTrip(t *testing.T) {
	orderID := id.New()
	variantID := "var-1"
	minted := download.OrderItemID(orderID, variantID)
	gotOrder, gotVar, ok := download.SplitOrderItemID(minted)
	if !ok || gotOrder != orderID || gotVar != variantID {
		t.Fatalf("Split = %q %q %v", gotOrder, gotVar, ok)
	}
	canon, ok := download.CanonicalOrderItemID(" " + strings.ToUpper(orderID) + " / " + variantID + " ")
	if !ok || canon != minted {
		t.Fatalf("Canonical = %q ok=%v, want %q", canon, ok, minted)
	}
	if _, ok := download.CanonicalOrderItemID("not-a-uuid/var"); ok {
		t.Fatal("non-UUID order must be rejected")
	}
}

func TestResolve_DefaultsAndOverrides(t *testing.T) {
	global := download.Defaults{Expiry: 24 * time.Hour, DownloadLimit: 5}
	exp, lim, err := download.Resolve(nil, global)
	if err != nil || exp != 24*time.Hour || lim != 5 {
		t.Fatalf("nil settings: %v %v %v", exp, lim, err)
	}
	days := 10
	limit := 3
	s := &download.GrantSettings{ProductID: id.New(), ExpiryDays: &days, DownloadLimit: &limit}
	exp, lim, err = download.Resolve(s, global)
	if err != nil || exp != 10*24*time.Hour || lim != 3 {
		t.Fatalf("override both: %v %v %v", exp, lim, err)
	}
	s2 := &download.GrantSettings{ProductID: id.New(), DownloadLimit: &limit}
	exp, lim, err = download.Resolve(s2, global)
	if err != nil || exp != 24*time.Hour || lim != 3 {
		t.Fatalf("override limit only: %v %v %v", exp, lim, err)
	}
	bad := 0
	_, _, err = download.Resolve(&download.GrantSettings{ProductID: id.New(), ExpiryDays: &bad}, global)
	if err == nil {
		t.Fatal("expected bad expiry_days error")
	}
	_, _, err = download.Resolve(nil, download.Defaults{Expiry: 0, DownloadLimit: 5})
	if err == nil {
		t.Fatal("expected bad global expiry error")
	}
}
