package b2b

import (
	"fmt"
	"strings"
)

const devKeyPrefix = "DEV-"

// Validate reports whether key entitles B2B module use.
//
// Stub behavior:
//   - empty key → (false, nil)
//   - DEV-* prefix → (true, nil) for local development
//   - anything else → (false, error) until online validation ships
//
// Production validation will call a license service and cache results.
func Validate(key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}
	if strings.HasPrefix(key, devKeyPrefix) {
		return true, nil
	}
	return false, fmt.Errorf("b2b license: online validation not implemented (contact vendor for a production key)")
}
