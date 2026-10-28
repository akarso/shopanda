package download

// Domain event names for DownloadGrant observability.
const (
	// EventDownloadGrantIssued is published when order.paid creates one or
	// more grants for a downloadable line (one event per grant row).
	EventDownloadGrantIssued = "download.grant.issued"

	// EventDownloadGrantRevoked is published when Revoke ends a grant
	// (admin path in PR-1078). Delivery does not publish this.
	EventDownloadGrantRevoked = "download.grant.revoked"
)

// GrantIssuedData is the payload for EventDownloadGrantIssued.
type GrantIssuedData struct {
	GrantID     string `json:"grant_id"`
	OrderItemID string `json:"order_item_id"`
	CustomerID  string `json:"customer_id"`
	AssetID     string `json:"asset_id"`
	OrderID     string `json:"order_id,omitempty"`
}

// GrantRevokedData is the payload for EventDownloadGrantRevoked.
type GrantRevokedData struct {
	GrantID     string `json:"grant_id"`
	OrderItemID string `json:"order_item_id"`
	CustomerID  string `json:"customer_id"`
	AssetID     string `json:"asset_id"`
	Reason      string `json:"reason,omitempty"`
}
