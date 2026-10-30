package postgres

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapProductAssignmentExecError(t *testing.T) {
	tests := []struct {
		code       string
		constraint string
		want       apperror.Code
	}{
		{"23505", productAssignmentsChildCurrentIndex, apperror.CodeConflict},
		{"23505", productAssignmentsPK, apperror.CodeConflict},
		{"23505", "", apperror.CodeConflict},
		{"23514", "", apperror.CodeValidation},
		{"22P02", "", apperror.CodeValidation},
	}
	for _, tc := range tests {
		t.Run(tc.code+"_"+tc.constraint, func(t *testing.T) {
			err := mapProductAssignmentExecError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
			if !apperror.Is(err, tc.want) {
				t.Fatalf("map(%s,%s) = %v, want %s", tc.code, tc.constraint, err, tc.want)
			}
		})
	}
	if err := mapProductAssignmentExecError(&pgconn.PgError{Code: "23503"}); apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("unmapped code should pass through, got %v", err)
	}
}
