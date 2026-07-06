package slots

import (
	"fmt"
	"strings"
)

// Placement describes where renderer HTML is injected relative to a slot anchor.
type Placement string

const (
	PlacementBefore  Placement = "before"
	PlacementAfter   Placement = "after"
	PlacementPrepend Placement = "prepend"
	PlacementAppend  Placement = "append"
)

// ParsePlacement normalizes and validates a placement string.
func ParsePlacement(raw string) (Placement, error) {
	switch Placement(strings.TrimSpace(strings.ToLower(raw))) {
	case PlacementBefore:
		return PlacementBefore, nil
	case PlacementAfter:
		return PlacementAfter, nil
	case PlacementPrepend:
		return PlacementPrepend, nil
	case PlacementAppend:
		return PlacementAppend, nil
	default:
		return "", fmt.Errorf("slots: unknown placement %q", raw)
	}
}
