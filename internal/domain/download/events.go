package download

// Domain event names for DownloadGrant observability.
const (
	// EventDownloadGrantIssued is published when order.paid creates one or
	// more grants for a downloadable line (one event per grant row).
	EventDownloadGrantIssued = "download.grant.issued"

	// EventDownloadGrantRevoked is published when Revoke ends a grant
	// (admin path in PR-1078). Delivery does not publish this.
	EventDownloadGrantRevoked = "download.grant.revoked"

	// EventDownloadGrantReissued is published after a successful support
	// re-issue (Extend + optional ResetCount) in PR-1078.
	EventDownloadGrantReissued = "download.grant.reissued"
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

// GrantReissuedData is the payload for EventDownloadGrantReissued.
type GrantReissuedData struct {
	GrantID       string `json:"grant_id"`
	OrderItemID   string `json:"order_item_id"`
	CustomerID    string `json:"customer_id"`
	AssetID       string `json:"asset_id"`
	ExpiresAt     string `json:"expires_at"`
	DownloadLimit int    `json:"download_limit"`
	ResetCount    bool   `json:"reset_count"`
	Reason        string `json:"reason,omitempty"`
}
