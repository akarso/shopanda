package invoice_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
)

func TestEventConstants(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{invoice.EventInvoiceCreated, "invoice.created"},
		{invoice.EventCreditNoteCreated, "credit_note.created"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("event = %q, want %q", tc.got, tc.want)
		}
	}
}
