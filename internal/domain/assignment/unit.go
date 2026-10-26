package assignment

import "strings"

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
