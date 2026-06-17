package customer

// Event names for the customer domain.
const (
	EventCustomerCreated               = "customer.created"
	EventCustomerDeleted               = "customer.deleted"
	EventEmailVerificationRequested    = "customer.email_verification.requested"
	EventPasswordResetRequested        = "customer.password_reset.requested"
	EventSecurityVerificationRequested = "customer.security_verification.requested"
	EventEmailChangeRequested          = "customer.email_change.requested"
	EventEmailChangeNotified           = "customer.email_change.notified"
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

// EmailChangeRequestedData is the payload for customer.email_change.requested.
// VerifyURL points to the confirmation link and must be delivered to NewEmail,
// the address the customer is switching to (not yet active on the account).
type EmailChangeRequestedData struct {
	CustomerID string `json:"customer_id"`
	NewEmail   string `json:"new_email"`
	VerifyURL  string `json:"verify_url"`
}

// EmailChangeNotifiedData is the payload for customer.email_change.notified.
// It is delivered to the current (old) address to alert the owner that a change
// was requested.
type EmailChangeNotifiedData struct {
	CustomerID string `json:"customer_id"`
	OldEmail   string `json:"old_email"`
	NewEmail   string `json:"new_email"`
}
