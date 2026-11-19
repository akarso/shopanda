package postgres

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestJobIDUnusable(t *testing.T) {
	if jobIDUnusable(id.New()) {
		t.Fatal("well-formed UUID must be usable")
	}
	for _, bad := range []string{"missing-id", "not-a-uuid", "", "abc"} {
		if !jobIDUnusable(bad) {
			t.Fatalf("%q: want unusable", bad)
		}
	}
}

func TestIsInvalidTextUUID(t *testing.T) {
	if !isInvalidTextUUID(&pgconn.PgError{Code: "22P02"}) {
		t.Fatal("22P02 must map")
	}
	if isInvalidTextUUID(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("23505 must not map")
	}
	if isInvalidTextUUID(nil) {
		t.Fatal("nil must not map")
	}
}
