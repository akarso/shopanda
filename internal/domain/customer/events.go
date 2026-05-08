package customer

// Event names for the customer domain.
const (
	EventCustomerCreated               = "customer.created"
	EventCustomerDeleted               = "customer.deleted"
	EventEmailVerificationRequested    = "customer.email_verification.requested"
	EventPasswordResetRequested        = "customer.password_reset.requested"
	EventSecurityVerificationRequested = "customer.security_verification.requested"
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

// EmailVerificationRequestedData is the payload for
// customer.email_verification.requested.
type EmailVerificationRequestedData struct {
	CustomerID string `json:"customer_id"`
	VerifyURL  string `json:"verify_url"`
}

// SecurityVerificationRequestedData is the payload for
// customer.security_verification.requested.
type SecurityVerificationRequestedData struct {
	CustomerID string `json:"customer_id"`
	VerifyURL  string `json:"verify_url"`
}
