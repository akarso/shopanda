package customer

// Event names for the customer domain.
const (
	EventCustomerCreated        = "customer.created"
	EventCustomerDeleted        = "customer.deleted"
	EventPasswordResetRequested = "customer.password_reset.requested"
)

// CustomerCreatedData is the payload for customer.created.
type CustomerCreatedData struct {
	CustomerID string `json:"customer_id"`
}

// CustomerDeletedData is the payload for customer.deleted.
type CustomerDeletedData struct {
	CustomerID string `json:"customer_id"`
}

// PasswordResetRequestedData is the payload for customer.password_reset.requested.
type PasswordResetRequestedData struct {
	CustomerID string `json:"customer_id"`
	Token      string `json:"token"`
}
