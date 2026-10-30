package postgres

import (
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapLinkedEligibilityExecError(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		constraint string
		want       apperror.Code
		msg        string
	}{
		{"23503 generic", "23503", "", apperror.CodeNotFound, "not found"},
		{"23503 category fk", "23503", "linked_eligible_categories_category_id_fkey", apperror.CodeNotFound, "category not found"},
		{"23503 child fk", "23503", "linked_eligible_parents_child_product_id_fkey", apperror.CodeNotFound, "product not found"},
		{"23514", "23514", "", apperror.CodeValidation, "linked eligibility constraint violation"},
		{"22P02", "22P02", "", apperror.CodeValidation, "invalid product id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapLinkedEligibilityExecError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
			if !apperror.Is(err, tc.want) {
				t.Fatalf("map(%s/%s) = %v, want %s", tc.code, tc.constraint, err, tc.want)
			}
			var appErr *apperror.Error
			if !errors.As(err, &appErr) || appErr.Message != tc.msg {
				got := ""
				if appErr != nil {
					got = appErr.Message
				}
				t.Fatalf("map(%s/%s) message = %q, want %q", tc.code, tc.constraint, got, tc.msg)
			}
		})
	}
	if err := mapLinkedEligibilityExecError(&pgconn.PgError{Code: "23505"}); apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("unmapped code should pass through, got %v", err)
	}
}
