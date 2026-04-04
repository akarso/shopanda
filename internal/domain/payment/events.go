package payment

// Event names for payment domain.
const (
	EventPaymentCreated   = "payment.created"
	EventPaymentCompleted = "payment.completed"
	EventPaymentFailed    = "payment.failed"
	EventPaymentRefunded  = "payment.refunded"
)

// PaymentCreatedData is emitted when a new payment is created.
type PaymentCreatedData struct {
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Method    string `json:"method"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
}

// PaymentStatusChangedData is emitted on status transitions.
type PaymentStatusChangedData struct {
	PaymentID   string `json:"payment_id"`
	OrderID     string `json:"order_id"`
	OldStatus   string `json:"old_status"`
	NewStatus   string `json:"new_status"`
	ProviderRef string `json:"provider_ref,omitempty"`
}
