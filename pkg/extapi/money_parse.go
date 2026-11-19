package extapi

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// IsValidCurrency reports whether code is a 3-letter uppercase ASCII ISO-like
// currency code (same rule as NewMoney).
func IsValidCurrency(code string) bool {
	return currencyRegex.MatchString(code)
}

// ParseMinorUnits parses a human decimal amount into minor units for the given
// currency exponent (0 = JPY, 2 = EUR/USD, 3 = KWD). No rounding: a fraction
// longer than exponent is rejected. Accepts only ASCII digits and "." / ","
// (e.g. "80,49", "1.234,56", "1,234.56"). Rejects empty input, spaces, letters,
// signs, non-ASCII digits, and a result of zero.
//
// Separator policy: when both "." and "," appear, the rightmost is the decimal
// separator. A single separator with at most `exponent` digits after it is
// always treated as decimal (so "1,234" with exponent 3 is 1234 fils, not one
// thousand — for thousands at exponent ≥ 3 use both separators, e.g.
// "1,234.567"). Grouped integers without a fraction ("1.234" / "1,234" at
// exponent 2) are rejected unless exponent is 0 (JPY-style thousands).
func ParseMinorUnits(s string, exponent int) (int64, error) {
	if exponent < 0 {
		return 0, fmt.Errorf("money: invalid currency exponent %d", exponent)
	}
	if s == "" {
		return 0, errors.New("money: empty amount")
	}
	for _, r := range s {
		if r == '.' || r == ',' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		return 0, fmt.Errorf("money: invalid amount character %q", r)
	}

	decSep, err := detectDecimalSep(s, exponent)
	if err != nil {
		return 0, err
	}

	intPart, fracPart, err := splitAmount(s, decSep)
	if err != nil {
		return 0, err
	}
	if err := validateGrouping(intPart); err != nil {
		return 0, err
	}
	intDigits := strings.ReplaceAll(strings.ReplaceAll(intPart, ".", ""), ",", "")
	if intDigits == "" {
		intDigits = "0"
	}
	if len(fracPart) > exponent {
		return 0, fmt.Errorf("money: fraction has %d digits, currency exponent is %d", len(fracPart), exponent)
	}
	if exponent > 0 {
		fracPart = fracPart + strings.Repeat("0", exponent-len(fracPart))
	}

	combined := intDigits + fracPart
	combined = strings.TrimLeft(combined, "0")
	if combined == "" {
		return 0, errors.New("money: amount must be non-zero")
	}

	var n int64
	for _, c := range combined {
		d := int64(c - '0')
		if n > (math.MaxInt64-d)/10 {
			return 0, errors.New("money: amount overflow")
		}
		n = n*10 + d
	}
	if n == 0 {
		return 0, errors.New("money: amount must be non-zero")
	}
	return n, nil
}

// detectDecimalSep returns the decimal separator rune, or 0 when there is no
// fractional part (grouping-only or plain digits).
func detectDecimalSep(s string, exponent int) (rune, error) {
	lastDot := strings.LastIndexByte(s, '.')
	lastComma := strings.LastIndexByte(s, ',')
	hasDot := lastDot >= 0
	hasComma := lastComma >= 0

	switch {
	case hasDot && hasComma:
		if lastDot > lastComma {
			return '.', nil
		}
		return ',', nil
	case hasDot:
		return singleSepDecimal(s, '.', exponent)
	case hasComma:
		return singleSepDecimal(s, ',', exponent)
	default:
		return 0, nil
	}
}

func singleSepDecimal(s string, sep byte, exponent int) (rune, error) {
	count := strings.Count(s, string(sep))
	if count == 0 {
		return 0, nil
	}
	if count > 1 {
		// Thousands grouping only; no fractional part.
		return 0, nil
	}
	i := strings.LastIndexByte(s, sep)
	fracLen := len(s) - i - 1
	if fracLen <= exponent {
		return rune(sep), nil
	}
	// Prefer rejecting extra fraction digits (no rounding). Zero-exponent
	// currencies still allow a single thousands group ("1.234" JPY).
	if exponent == 0 && fracLen == 3 {
		return 0, nil
	}
	return 0, fmt.Errorf("money: fraction has %d digits, currency exponent is %d", fracLen, exponent)
}

func splitAmount(s string, decSep rune) (intPart, fracPart string, err error) {
	if decSep == 0 {
		return s, "", nil
	}
	i := strings.LastIndexByte(s, byte(decSep))
	if i < 0 {
		return s, "", nil
	}
	intPart = s[:i]
	fracPart = s[i+1:]
	if strings.ContainsRune(fracPart, '.') || strings.ContainsRune(fracPart, ',') {
		return "", "", errors.New("money: invalid separators in fraction")
	}
	if fracPart != "" {
		for i := 0; i < len(fracPart); i++ {
			c := fracPart[i]
			if c < '0' || c > '9' {
				return "", "", errors.New("money: invalid fraction")
			}
		}
	}
	return intPart, fracPart, nil
}

func validateGrouping(intPart string) error {
	if intPart == "" {
		return nil
	}
	// Disallow leading separators or empty groups.
	if intPart[0] == '.' || intPart[0] == ',' {
		return errors.New("money: invalid integer grouping")
	}
	var digitsSinceSep int
	seps := 0
	for i := len(intPart) - 1; i >= 0; i-- {
		c := intPart[i]
		switch {
		case c >= '0' && c <= '9':
			digitsSinceSep++
		case c == '.' || c == ',':
			if digitsSinceSep == 0 {
				return errors.New("money: empty integer group")
			}
			if seps > 0 && digitsSinceSep != 3 {
				return errors.New("money: invalid integer grouping")
			}
			if seps == 0 && digitsSinceSep > 3 {
				// Rightmost group before a sep may be 1–3 digits only when
				// further groups exist; a single trailing group of >3 with a
				// sep elsewhere is already handled. First (rightmost) group
				// after walking must be exactly 3 when any sep is present.
				return errors.New("money: invalid integer grouping")
			}
			if seps == 0 && digitsSinceSep != 3 {
				return errors.New("money: invalid integer grouping")
			}
			seps++
			digitsSinceSep = 0
		default:
			return errors.New("money: invalid integer part")
		}
	}
	if seps > 0 && digitsSinceSep == 0 {
		return errors.New("money: invalid integer grouping")
	}
	return nil
}
