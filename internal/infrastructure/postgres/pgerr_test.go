package postgres

import (
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestInvalidUUIDError(t *testing.T) {
	mapped := invalidUUIDError(&pgconn.PgError{Code: "22P02"}, "invalid order id")
	if !apperror.Is(mapped, apperror.CodeValidation) || mapped.(*apperror.Error).Message != "invalid order id" {
		t.Fatalf("22P02 = %v", mapped)
	}
	if got := invalidUUIDError(&pgconn.PgError{Code: "23505"}, "invalid order id"); got != nil {
		t.Fatalf("other code = %v", got)
	}
	if got := invalidUUIDError(errors.New("db down"), "invalid order id"); got != nil {
		t.Fatalf("plain error = %v", got)
	}
}
