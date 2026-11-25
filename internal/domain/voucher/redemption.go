package voucher

import (
	"errors"
	"strings"
	"time"
)

// Redemption records one staff redeem or unredeem action.
type Redemption struct {
	ID        string
	VoucherID string
	Action    string // "redeem" | "unredeem"
	ActorID   string // admin user id
	Note      string
	CreatedAt time.Time
}

const (
	ActionRedeem   = "redeem"
	ActionUnredeem = "unredeem"
	// MaxNoteLen caps staff notes on redeem/unredeem.
	MaxNoteLen = 500
)

// NewRedemption builds a redeem/unredeem audit row.
func NewRedemption(id, voucherID, action, actorID, note string, at time.Time) (*Redemption, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	r := &Redemption{
		ID:        strings.TrimSpace(id),
		VoucherID: strings.TrimSpace(voucherID),
		Action:    strings.TrimSpace(action),
		ActorID:   strings.TrimSpace(actorID),
		Note:      strings.TrimSpace(note),
		CreatedAt: at.UTC(),
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Validate checks redemption fields.
func (r *Redemption) Validate() error {
	if r == nil {
		return errors.New("voucher redemption must not be nil")
	}
	r.ID = strings.TrimSpace(r.ID)
	r.VoucherID = strings.TrimSpace(r.VoucherID)
	r.Action = strings.TrimSpace(r.Action)
	r.ActorID = strings.TrimSpace(r.ActorID)
	r.Note = strings.TrimSpace(r.Note)
	if r.ID == "" {
		return errors.New("voucher redemption: id must not be empty")
	}
	if r.VoucherID == "" {
		return errors.New("voucher redemption: voucher id must not be empty")
	}
	if r.Action != ActionRedeem && r.Action != ActionUnredeem {
		return errors.New("voucher redemption: action must be redeem or unredeem")
	}
	if r.ActorID == "" {
		return errors.New("voucher redemption: actor id must not be empty")
	}
	if len(r.Note) > MaxNoteLen {
		return errors.New("voucher redemption: note exceeds maximum length")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("voucher redemption: created_at must not be zero")
	}
	return nil
}
