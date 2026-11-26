package postgres

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapInvoiceInsertError(t *testing.T) {
	err := mapInvoiceInsertError(&pgconn.PgError{Code: "23505", ConstraintName: "invoices_order_id_key"})
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("23505 = %v, want Conflict", err)
	}
	if mapped := mapInvoiceInsertError(&pgconn.PgError{Code: "23503"}); mapped != nil {
		t.Fatalf("unrelated code mapped = %v", mapped)
	}
	if mapped := mapInvoiceInsertError(nil); mapped != nil {
		t.Fatalf("nil mapped = %v", mapped)
	}
}
