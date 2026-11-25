package voucher

// Domain event names for voucher lifecycle.
const (
	EventVoucherIssued     = "voucher.issued"
	EventVoucherRedeemed   = "voucher.redeemed"
	EventVoucherUnredeemed = "voucher.unredeemed"
	EventVoucherVoided     = "voucher.voided"
)

// IssuedData is published after codes are created for a paid order.
type IssuedData struct {
	OrderID    string   `json:"order_id"`
	CustomerID string   `json:"customer_id"`
	VoucherIDs []string `json:"voucher_ids"`
}

// RedeemedData is published after a successful redeem or unredeem.
type RedeemedData struct {
	VoucherID string `json:"voucher_id"`
	OrderID   string `json:"order_id"`
	Code      string `json:"code"`
	ActorID   string `json:"actor_id"`
	Action    string `json:"action"`
}

// VoidedData is published after vouchers are voided on return approve/refund.
type VoidedData struct {
	VoucherID   string `json:"voucher_id"`
	OrderID     string `json:"order_id"`
	OrderItemID string `json:"order_item_id"`
	Reason      string `json:"reason"`
}
