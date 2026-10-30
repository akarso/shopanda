package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapDownloadGrantExecError(t *testing.T) {
	tests := []struct {
		code       string
		constraint string
		want       apperror.Code
	}{
		{"23505", downloadGrantsOrderItemAssetUniq, apperror.CodeConflict},
		{"23505", downloadGrantsPK, apperror.CodeConflict},
		{"23505", "", apperror.CodeConflict},
		{"23503", "", apperror.CodeNotFound},
		{"23514", "", apperror.CodeValidation},
		{"22P02", "", apperror.CodeValidation},
	}
	for _, tc := range tests {
		t.Run(tc.code+"_"+tc.constraint, func(t *testing.T) {
			err := mapDownloadGrantExecError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
			if !apperror.Is(err, tc.want) {
				t.Fatalf("map(%s,%s) = %v, want %s", tc.code, tc.constraint, err, tc.want)
			}
		})
	}
	if err := mapDownloadGrantExecError(&pgconn.PgError{Code: "42P01"}); apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("unmapped code should pass through, got %v", err)
	}
}

func TestDownloadGrantRepo_NilDB(t *testing.T) {
	_, err := NewDownloadGrantRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestDownloadableGrantSettingsRepo_NilDB(t *testing.T) {
	_, err := NewDownloadableGrantSettingsRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestDownloadGrantRepo_FindByID_InvalidUUID(t *testing.T) {
	// No DSN needed — validation runs before the query.
	repo := &DownloadGrantRepo{db: nil}
	_, err := repo.FindByID(context.Background(), "not-a-uuid")
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("FindByID invalid uuid = %v", err)
	}
	_ = errors.Is(err, download.ErrCountConflict)
}
