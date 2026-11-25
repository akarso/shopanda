package store

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

// Identity is the legal / contact presence for one store (Magento-like Store Information).
// It is distinct from Store (commerce context: code, currency, country, DBA name).
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
// must parse as RFC 5322 addresses. storeID is required.
func NewIdentity(
	storeID, legalName, phone, hours, emailGeneral, emailSales, emailSupport string,
) (Identity, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return Identity{}, errors.New("store identity: store id must not be empty")
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
		LegalName:    strings.TrimSpace(legalName),
		Phone:        strings.TrimSpace(phone),
		Hours:        strings.TrimSpace(hours),
		EmailGeneral: general,
		EmailSales:   sales,
		EmailSupport: support,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

// NewIdentityFromDB reconstructs Identity from persistence (no re-validation).
func NewIdentityFromDB(
	storeID, legalName, phone, hours, emailGeneral, emailSales, emailSupport string,
	updatedAt time.Time,
) Identity {
	return Identity{
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

func normalizeOptionalEmail(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "", nil
	}
	if _, err := mail.ParseAddress(v); err != nil {
		return "", err
	}
	return v, nil
}
