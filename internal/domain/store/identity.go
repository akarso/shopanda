package store

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Field length caps for identity / address / tax constructors (API and DB columns
// in PR-1202 follow these).
const (
	maxEmailLen       = 254
	maxLegalNameLen   = 255
	maxHoursLen       = 255
	maxPhoneLen       = 64
	maxAddressLineLen = 255
	maxCityLen        = 255
	maxRegionLen      = 255
	maxPostcodeLen    = 64
	maxTaxTypeLen     = 64
	maxTaxValueLen    = 64
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
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return Identity{}, errors.New("store identity: store id must not be empty")
	}
	legal, err := normalizeSingleLine(legalName, maxLegalNameLen, "legal name")
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	phoneNorm, err := normalizeSingleLine(phone, maxPhoneLen, "phone")
	if err != nil {
		return Identity{}, fmt.Errorf("store identity: %w", err)
	}
	hoursNorm, err := normalizeBounded(hours, maxHoursLen, "hours")
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
		StoreID:      storeID,
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

// normalizeOptionalEmail returns the mailbox address only (lowercased).
func normalizeOptionalEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(v)
	if err != nil {
		return "", err
	}
	out := strings.ToLower(strings.TrimSpace(addr.Address))
	if out == "" {
		return "", errors.New("empty mailbox")
	}
	if utf8.RuneCountInString(out) > maxEmailLen {
		return "", errors.New("email too long")
	}
	return out, nil
}

// normalizeSingleLine trims, rejects interior newlines/CRs, and caps length.
func normalizeSingleLine(v string, max int, field string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n") {
		return "", fmt.Errorf("%s must not contain newlines", field)
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
