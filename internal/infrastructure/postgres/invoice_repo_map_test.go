package postgres

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapInvoiceInsertError(t *testing.T) {
	err := mapInvoiceInsertError(&pgconn.PgError{Code: "23505", ConstraintName: invoicesOrderIDUniq})
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("order_id 23505 = %v, want Conflict", err)
	}
	if mapped := mapInvoiceInsertError(&pgconn.PgError{Code: "23505", ConstraintName: "invoices_pkey"}); mapped != nil {
		t.Fatalf("pk 23505 must not map to Conflict, got %v", mapped)
	}
	if mapped := mapInvoiceInsertError(&pgconn.PgError{Code: "23505", ConstraintName: "invoices_invoice_number_key"}); mapped != nil {
		t.Fatalf("invoice_number 23505 must not map to Conflict, got %v", mapped)
	}
	if mapped := mapInvoiceInsertError(&pgconn.PgError{Code: "23503"}); mapped != nil {
		t.Fatalf("unrelated code mapped = %v", mapped)
	}
}
