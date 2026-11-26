package store

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Field length caps for identity / address / tax constructors (API and DB
// columns in PR-1202 must use these, not hard-coded duplicates).
const (
	MaxStoreIDLen     = 64
	MaxEmailLen       = 254
	MaxEmailInputLen  = MaxEmailLen * 4 // raw input before ParseAddress (display name discarded)
	MaxLegalNameLen   = 255
	MaxHoursLen       = 255
	MaxPhoneLen       = 64
	MaxAddressLineLen = 255
	MaxCityLen        = 255
	MaxRegionLen      = 255
	MaxPostcodeLen    = 64
	MaxTaxTypeLen     = 64
	MaxTaxValueLen    = 64
	MaxAddressRoleLen = 64
)

// Identity is the legal / contact presence for one store (Magento-like Store Information).
// It is distinct from Store (commerce context: code, currency, country, DBA name).
//
// Empty LegalName is allowed for drafts; invoice/imprint consumers must treat
// empty as missing (no domain-level required guard).
type Identity struct {
	StoreID      string
	LegalName    string
	Phone        string
	Hours        string
	EmailGeneral string
	EmailSales   string
	EmailSupport string
	UpdatedAt    time.Time
}

// NewIdentity constructs a store identity. Emails may be empty; when set they
// must parse as RFC 5322 and are stored as the mailbox only (addr.Address,
// lowercased) — the same shape as auth and the sales → general → smtp.from
// fallback. storeID is required. Legal name may be empty (draft).
func NewIdentity(
	storeID, legalName, phone, hours, emailGeneral, emailSales, emailSupport string,
) (Identity, error) {
	sid, err := normalizeStoreID(storeID)
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	legal, err := normalizeSingleLine(legalName, MaxLegalNameLen, "legal name")
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	phoneNorm, err := normalizeSingleLine(phone, MaxPhoneLen, "phone")
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	hoursNorm, err := normalizeBounded(hours, MaxHoursLen, "hours")
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	general, err := normalizeOptionalEmail(emailGeneral)
	if err != nil {
		return Identity{}, errors.New("store identity: invalid general email")
	}
	sales, err := normalizeOptionalEmail(emailSales)
	if err != nil {
		return Identity{}, errors.New("store identity: invalid sales email")
	}
	support, err := normalizeOptionalEmail(emailSupport)
	if err != nil {
		return Identity{}, errors.New("store identity: invalid support email")
	}
	return Identity{
		StoreID:      sid,
		LegalName:    legal,
		Phone:        phoneNorm,
		Hours:        hoursNorm,
		EmailGeneral: general,
		EmailSales:   sales,
		EmailSupport: support,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

// NewIdentityFromDB reconstructs Identity from persistence (no re-validation).
// Matches NewStoreFromDB pointer style. Invalid or oversized values are kept
// as stored so reload stays explicitly unvalidated.
func NewIdentityFromDB(
	storeID, legalName, phone, hours, emailGeneral, emailSales, emailSupport string,
	updatedAt time.Time,
) *Identity {
	return &Identity{
		StoreID:      storeID,
		LegalName:    legalName,
		Phone:        phone,
		Hours:        hours,
		EmailGeneral: emailGeneral,
		EmailSales:   emailSales,
		EmailSupport: emailSupport,
		UpdatedAt:    updatedAt,
	}
}

func normalizeStoreID(v string) (string, error) {
	id, err := normalizeSingleLine(v, MaxStoreIDLen, "store id")
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("store id must not be empty")
	}
	return id, nil
}

// normalizeOptionalEmail returns the mailbox address only (lowercased).
func normalizeOptionalEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if utf8.RuneCountInString(v) > MaxEmailInputLen {
		return "", errors.New("email input too long")
	}
	addr, err := mail.ParseAddress(v)
	if err != nil {
		return "", err
	}
	out := strings.ToLower(strings.TrimSpace(addr.Address))
	if out == "" {
		return "", errors.New("empty mailbox")
	}
	if utf8.RuneCountInString(out) > MaxEmailLen {
		return "", errors.New("email too long")
	}
	return out, nil
}

// hasDisallowedLineControl reports C0 controls and Unicode line breaks that
// must not appear in single-line seller-block fields.
func hasDisallowedLineControl(v string) bool {
	for _, r := range v {
		switch {
		case r == 0 || r == '\n' || r == '\r' || r == '\v' || r == '\f':
			return true
		case r == 0x85 || r == 0x2028 || r == 0x2029: // NEL, LS, PS
			return true
		case r < 0x20: // remaining C0 (incl. TAB)
			return true
		case r == 0x7F: // DEL
			return true
		}
	}
	return false
}

// normalizeSingleLine trims, rejects line breaks / C0 controls / NUL, and caps length.
func normalizeSingleLine(v string, max int, field string) (string, error) {
	v = strings.TrimSpace(v)
	if hasDisallowedLineControl(v) {
		return "", fmt.Errorf("%s must not contain line breaks or control characters", field)
	}
	if utf8.RuneCountInString(v) > max {
		return "", fmt.Errorf("%s exceeds max length %d", field, max)
	}
	return v, nil
}

// normalizeBounded trims and caps length; newlines are allowed (e.g. hours).
func normalizeBounded(v string, max int, field string) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max {
		return "", fmt.Errorf("%s exceeds max length %d", field, max)
	}
	return v, nil
}
