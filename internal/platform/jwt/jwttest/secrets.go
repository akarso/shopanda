// Package jwttest holds JWT test helpers that must not ship in the runtime binary.
package jwttest

// TestSecret is a valid 64-hex JWT secret for unit tests (installer shape).
const TestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// TestSecretOther is a second valid secret for negative signature tests.
const TestSecretOther = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
