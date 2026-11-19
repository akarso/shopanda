package postgres

import (
	"errors"

	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

// jobIDUnusable reports whether jobID cannot be bound to jobs.id (uuid).
// Callers treat this like a miss so operators never see SQLSTATE 22P02.
func jobIDUnusable(jobID string) bool {
	return !id.IsUUID(jobID)
}

// isInvalidTextUUID reports PostgreSQL 22P02 (invalid_text_representation),
// the residual path when a value slips past jobIDUnusable.
func isInvalidTextUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
