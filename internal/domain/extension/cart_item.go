package extension

import "strings"

// TargetCartItemMergeBackup is an internal target_type for durable pre-merge
// cart_item snapshots (guest claim compensation). Not a field scope — core
// writes raw Value rows here so a failed ReplaceTarget after CopyTarget
// survives process restart. ID shape: customerCartID:guestCartID:variantID.
const TargetCartItemMergeBackup TargetType = "cart_item_merge_backup"

// MergeBackupTargetID returns the target_id for a pre-merge backup row set.
func MergeBackupTargetID(customerCartID, guestCartID, variantID string) string {
	return strings.TrimSpace(customerCartID) + ":" + strings.TrimSpace(guestCartID) + ":" + strings.TrimSpace(variantID)
}

// MergeBackupTarget is the Target for a pre-merge extension snapshot.
func MergeBackupTarget(customerCartID, guestCartID, variantID string) Target {
	return Target{Type: TargetCartItemMergeBackup, ID: MergeBackupTargetID(customerCartID, guestCartID, variantID)}
}

// ParseMergeBackupTargetID splits customerCartID:guestCartID:variantID.
func ParseMergeBackupTargetID(targetID string) (customerCartID, guestCartID, variantID string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(targetID), ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// CartItemTargetID returns the stable target_id for a cart line extension value.
func CartItemTargetID(cartID, variantID string) string {
	return strings.TrimSpace(cartID) + ":" + strings.TrimSpace(variantID)
}

// CartItemTarget builds a cart_item extension target for cartID and variantID.
func CartItemTarget(cartID, variantID string) Target {
	return Target{
		Type: TargetCartItem,
		ID:   CartItemTargetID(cartID, variantID),
	}
}

// ParseCartItemTargetID splits a cart_item target_id on the first ':'.
func ParseCartItemTargetID(targetID string) (cartID, variantID string, ok bool) {
	targetID = strings.TrimSpace(targetID)
	i := strings.IndexByte(targetID, ':')
	if i <= 0 || i >= len(targetID)-1 {
		return "", "", false
	}
	cartID = strings.TrimSpace(targetID[:i])
	variantID = strings.TrimSpace(targetID[i+1:])
	if cartID == "" || variantID == "" {
		return "", "", false
	}
	return cartID, variantID, true
}
