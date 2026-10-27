package postgres

import (
	"errors"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// invalidUUIDError maps PostgreSQL 22P02 (invalid_text_representation) to
// a validation error. Order and variant primary keys are UUID columns;
// a non-UUID lookup must not surface as an internal error.
func invalidUUIDError(err error, message string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return apperror.Validation(message)
	}
	return nil
}
