package postgres

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapGroupedMemberExecError(t *testing.T) {
	tests := []struct {
		code string
		want apperror.Code
	}{
		{"23503", apperror.CodeNotFound},
		{"23514", apperror.CodeValidation},
		{"22P02", apperror.CodeValidation},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			err := mapGroupedMemberExecError(&pgconn.PgError{Code: tc.code})
			if !apperror.Is(err, tc.want) {
				t.Fatalf("map(%s) = %v, want %s", tc.code, err, tc.want)
			}
		})
	}
	if err := mapGroupedMemberExecError(&pgconn.PgError{Code: "23505"}); apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("unmapped code should pass through, got %v", err)
	}
}
