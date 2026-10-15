package catalog

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// MaxListCursorOffset caps repository OFFSET and remaining-skip values
// accepted or emitted in opaque after cursors until keyset pagination
// lands (PR-1057). It does not bound classic ?offset= listing cost —
// only cursor resume depth.
const MaxListCursorOffset = 100_000

// ErrInvalidListCursor is returned when an after cursor cannot be decoded.
var ErrInvalidListCursor = errors.New("invalid after cursor")

// ErrListCursorTooDeep is returned when an after cursor's offset or
// remaining skip exceeds MaxListCursorOffset.
var ErrListCursorTooDeep = errors.New("after exceeds maximum of 100000")

type listCursor struct {
	O int `json:"o"` // repository OFFSET
	S int `json:"s"` // remaining visible-product skip
}

// EncodeListCursor builds an opaque after cursor. skipRemaining is the
// VisibleSkip still owed when a prior scan budget stopped mid-skip.
// Callers must not emit cursors with values above MaxListCursorOffset;
// use NextAfterCursor.
func EncodeListCursor(dbOffset, skipRemaining int) string {
	if dbOffset < 0 {
		dbOffset = 0
	}
	if skipRemaining < 0 {
		skipRemaining = 0
	}
	raw, _ := json.Marshal(listCursor{O: dbOffset, S: skipRemaining})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// NextAfterCursor returns an opaque resume cursor, or "" when the offset
// or remaining skip would exceed MaxListCursorOffset. ListVisiblePage uses
// this so the server never issues a cursor DecodeListCursor would reject.
func NextAfterCursor(dbOffset, skipRemaining int) string {
	if dbOffset < 0 || skipRemaining < 0 {
		return ""
	}
	if dbOffset > MaxListCursorOffset || skipRemaining > MaxListCursorOffset {
		return ""
	}
	return EncodeListCursor(dbOffset, skipRemaining)
}

// DecodeListCursor parses an opaque after cursor.
func DecodeListCursor(s string) (dbOffset, skipRemaining int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, 0, ErrInvalidListCursor
	}
	var c listCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, 0, ErrInvalidListCursor
	}
	if c.O < 0 || c.S < 0 {
		return 0, 0, ErrInvalidListCursor
	}
	if c.O > MaxListCursorOffset || c.S > MaxListCursorOffset {
		return 0, 0, ErrListCursorTooDeep
	}
	return c.O, c.S, nil
}
