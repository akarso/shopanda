package customer_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
)

func TestEventConstants(t *testing.T) {
	if customer.EventCustomerCreated != "customer.created" {
		t.Errorf("EventCustomerCreated = %q", customer.EventCustomerCreated)
	}
	if customer.EventPasswordResetRequested != "customer.password_reset.requested" {
		t.Errorf("EventPasswordResetRequested = %q", customer.EventPasswordResetRequested)
	}
}
