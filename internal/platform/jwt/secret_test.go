package jwt_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
)

func TestParseSecret_Empty(t *testing.T) {
	_, err := jwt.ParseSecret("")
	if err == nil || !strings.Contains(err.Error(), jwt.EnvJWTSecret) {
		t.Fatalf("empty: err=%v, want %s named error", err, jwt.EnvJWTSecret)
	}
	_, err = jwt.ParseSecret("   \n\t  ")
	if err == nil || !strings.Contains(err.Error(), jwt.EnvJWTSecret) {
		t.Fatalf("whitespace: err=%v, want %s named error", err, jwt.EnvJWTSecret)
	}
}

func TestParseSecret_ShortAndOddHex(t *testing.T) {
	for _, raw := range []string{
		"short",
		"0123456789abcdef",                // 16 hex chars
		"0123456789abcdef0123456789abcde", // 31 chars
		"abcdef0123456789abcdef012345678", // 31 hex-ish
		"xyz",                             // odd short
	} {
		_, err := jwt.ParseSecret(raw)
		if err == nil || !strings.Contains(err.Error(), jwt.EnvJWTSecret) {
			t.Fatalf("ParseSecret(%q) err=%v, want named rejection", raw, err)
		}
	}
}

func TestParseSecret_Accepts64HexAsRawKeyMaterial(t *testing.T) {
	raw := jwttest.TestSecret
	got, err := jwt.ParseSecret(raw)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	// Must keep the hex text as key material (prior-release compatible).
	if !bytes.Equal(got, []byte(raw)) {
		t.Fatalf("64-hex must not be decoded; got len=%d want raw ASCII hex", len(got))
	}
}

func TestParseSecret_Accepts64HexWithNewline(t *testing.T) {
	got, err := jwt.ParseSecret(jwttest.TestSecret + "\n")
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	if !bytes.Equal(got, []byte(jwttest.TestSecret)) {
		t.Fatal("newline trim should yield trimmed hex text as key")
	}
}

func TestParseSecret_AcceptsRawAtLeast32(t *testing.T) {
	raw := strings.Repeat("a", 32)
	got, err := jwt.ParseSecret(raw)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	if !bytes.Equal(got, []byte(raw)) {
		t.Fatal("raw ≥32 should be used as-is")
	}
	raw64 := strings.Repeat("g", 64)
	got, err = jwt.ParseSecret(raw64)
	if err != nil {
		t.Fatalf("ParseSecret non-hex 64: %v", err)
	}
	if !bytes.Equal(got, []byte(raw64)) {
		t.Fatal("non-hex 64-char secret must stay raw")
	}
}
