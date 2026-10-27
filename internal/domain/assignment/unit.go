package assignment

import (
	"strings"

	"github.com/akarso/shopanda/internal/platform/id"
)

const unitIDSep = "/"

// UnitID mints the opaque id for an order line. Format is
// orderID + "/" + variantID. Both parts are trimmed. An empty part
// yields an empty string so Validate rejects it later.
func UnitID(orderID, variantID string) string {
	orderID = strings.TrimSpace(orderID)
	variantID = strings.TrimSpace(variantID)
	if orderID == "" || variantID == "" {
		return ""
	}
	return orderID + unitIDSep + variantID
}

// SplitUnitID parses a UnitID minted by UnitID. ok is false when the
// string is empty or has no separator. Extra slashes stay in variantID
// (order ids are UUIDs and do not contain "/").
func SplitUnitID(unitID string) (orderID, variantID string, ok bool) {
	unitID = strings.TrimSpace(unitID)
	if unitID == "" {
		return "", "", false
	}
	orderID, variantID, found := strings.Cut(unitID, unitIDSep)
	orderID = strings.TrimSpace(orderID)
	variantID = strings.TrimSpace(variantID)
	if !found || orderID == "" || variantID == "" {
		return "", "", false
	}
	return orderID, variantID, true
}

// CanonicalUnitID rebuilds a unit id after SplitUnitID so padded or
// spaced input matches the minted orderID/variantID form used in writes.
// The order part must be a UUID. It is lowercased so an uppercase order
// UUID hits the same row as the lowercase id minted at purchase. The
// variant part is left as written. A non-UUID order id is rejected here
// so callers do not send it to a UUID column (PostgreSQL 22P02).
func CanonicalUnitID(unitID string) (string, bool) {
	orderID, variantID, ok := SplitUnitID(unitID)
	if !ok || !id.IsUUID(orderID) {
		return "", false
	}
	orderID = strings.ToLower(orderID)
	return UnitID(orderID, variantID), true
}
