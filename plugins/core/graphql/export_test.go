package graphql

import (
	"database/sql"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// SetVisibilityFactsSourceForTest re-exports setVisibilityFactsSourceForTest for
// external tests (compiled only during `go test`).
func SetVisibilityFactsSourceForTest(fn func(db *sql.DB) (catalog.VisibilityFactsSource, error)) func() {
	return setVisibilityFactsSourceForTest(fn)
}
