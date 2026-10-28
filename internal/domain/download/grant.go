package download

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// MaxDownloadLimit is the hard cap for DownloadLimit (DB CHECK mirrors this).
	MaxDownloadLimit = 10000
	// MaxExpiryDays is the hard cap for product-level expiry overrides and
	// global catalog.downloadable.default_expiry.
	MaxExpiryDays = 3650
)

// Sentinel errors for delivery gates. Service.RecordDownload maps these with
// errors.Is — do not string-match messages.
var (
	ErrExpired   = errors.New("download grant: expired")
	ErrExhausted = errors.New("download grant: download limit reached")
)

// DownloadGrant is a post-purchase entitlement to download one media asset
// from a paid downloadable order line (PR-1075). It is deliberately not a
// ProductAssignment — there is no parent-binding step. Delivery that
// consumes CanDownload / RecordDownload is PR-1077; admin revoke/re-issue
// HTTP is PR-1078.
type DownloadGrant struct {
	ID            string
	OrderItemID   string // opaque; typically orderID/variantID
	CustomerID    string // empty for guest checkouts until account link
	AssetID       string
	ExpiresAt     time.Time
	DownloadLimit int
	DownloadCount int
	CreatedAt     time.Time
}

// NewDownloadGrant builds a grant with DownloadCount 0. IDs are trimmed;
// field validation runs before return. expiresAt and createdAt are stored UTC.
func NewDownloadGrant(id, orderItemID, customerID, assetID string, expiresAt time.Time, downloadLimit int) (*DownloadGrant, error) {
	now := time.Now().UTC()
	g := &DownloadGrant{
		ID:            strings.TrimSpace(id),
		OrderItemID:   strings.TrimSpace(orderItemID),
		CustomerID:    strings.TrimSpace(customerID),
		AssetID:       strings.TrimSpace(assetID),
		ExpiresAt:     expiresAt.UTC(),
		DownloadLimit: downloadLimit,
		DownloadCount: 0,
		CreatedAt:     now,
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return g, nil
}

// Validate checks field-level constraints. It trims ID fields in place so
// Upsert callers that skip NewDownloadGrant still persist clean IDs.
func (g *DownloadGrant) Validate() error {
	if g == nil {
		return errors.New("download grant must not be nil")
	}
	g.ID = strings.TrimSpace(g.ID)
	g.OrderItemID = strings.TrimSpace(g.OrderItemID)
	g.CustomerID = strings.TrimSpace(g.CustomerID)
	g.AssetID = strings.TrimSpace(g.AssetID)
	if g.ID == "" {
		return errors.New("download grant: id must not be empty")
	}
	if g.OrderItemID == "" {
		return errors.New("download grant: order item id must not be empty")
	}
	if g.AssetID == "" {
		return errors.New("download grant: asset id must not be empty")
	}
	if g.ExpiresAt.IsZero() {
		return errors.New("download grant: expires_at must not be zero")
	}
	if g.DownloadLimit < 1 {
		return errors.New("download grant: download_limit must be at least 1")
	}
	if g.DownloadLimit > MaxDownloadLimit {
		return fmt.Errorf("download grant: download_limit must not exceed %d", MaxDownloadLimit)
	}
	if g.DownloadCount < 0 {
		return errors.New("download grant: download_count must not be negative")
	}
	if g.DownloadCount > g.DownloadLimit {
		return errors.New("download grant: download_count must not exceed download_limit")
	}
	if g.CreatedAt.IsZero() {
		return errors.New("download grant: created_at must not be zero")
	}
	return nil
}

// Expired reports whether now is at or after ExpiresAt.
func (g *DownloadGrant) Expired(now time.Time) bool {
	if g == nil {
		return true
	}
	return !now.UTC().Before(g.ExpiresAt.UTC())
}

// Exhausted reports whether DownloadCount has reached DownloadLimit.
func (g *DownloadGrant) Exhausted() bool {
	if g == nil {
		return true
	}
	return g.DownloadCount >= g.DownloadLimit
}

// CanDownload is the delivery gate PR-1077 will call before streaming.
// It does not mutate state.
func (g *DownloadGrant) CanDownload(now time.Time) error {
	if g == nil {
		return errors.New("download grant must not be nil")
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if g.Expired(now) {
		return ErrExpired
	}
	if g.Exhausted() {
		return ErrExhausted
	}
	return nil
}

// RecordDownload increments DownloadCount after a successful delivery
// attempt is accepted. Call only after CanDownload succeeds for the same
// now. Persistence is the caller's job (optimistic update in the repo).
func (g *DownloadGrant) RecordDownload(now time.Time) error {
	if err := g.CanDownload(now); err != nil {
		return err
	}
	g.DownloadCount++
	return nil
}

// Extend moves ExpiresAt forward (or resets it). expiresAt must be after
// CreatedAt. Used by support re-issue paths (PR-1078).
func (g *DownloadGrant) Extend(expiresAt time.Time) error {
	if g == nil {
		return errors.New("download grant must not be nil")
	}
	expiresAt = expiresAt.UTC()
	if expiresAt.IsZero() {
		return errors.New("download grant: expires_at must not be zero")
	}
	if !expiresAt.After(g.CreatedAt.UTC()) {
		return errors.New("download grant: expires_at must be after created_at")
	}
	prev := g.ExpiresAt
	g.ExpiresAt = expiresAt
	if err := g.Validate(); err != nil {
		g.ExpiresAt = prev
		return err
	}
	return nil
}

// Revoke ends the grant immediately by setting ExpiresAt to now (no
// separate revoked column — matches the drafted schema). An already-expired
// grant is left unchanged (idempotent). download_count is not cleared so
// history of use remains.
func (g *DownloadGrant) Revoke(now time.Time) error {
	if g == nil {
		return errors.New("download grant must not be nil")
	}
	now = now.UTC()
	if g.Expired(now) {
		return nil
	}
	prev := g.ExpiresAt
	g.ExpiresAt = now
	if err := g.Validate(); err != nil {
		g.ExpiresAt = prev
		return err
	}
	return nil
}

// ResetCount sets DownloadCount to 0 for a support re-issue. Limit and
// expiry are unchanged unless the caller also Extend's.
func (g *DownloadGrant) ResetCount() error {
	if g == nil {
		return errors.New("download grant must not be nil")
	}
	g.DownloadCount = 0
	return g.Validate()
}
