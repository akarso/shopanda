package store

import "strings"

// PreferContactEmail returns sales when set, otherwise general (display/reply).
// Envelope From stays mail.smtp.from — these are identities shown to humans.
func PreferContactEmail(sales, general string) string {
	if s := strings.TrimSpace(sales); s != "" {
		return s
	}
	return strings.TrimSpace(general)
}

// FormatAddressDisplay formats structured address parts for email layout and
// storefront footer. Spec shape: "line1, line2, postcode city, country".
// Empty parts are skipped. Region is included when present (between line2 and
// the postcode/city group) so admin summaries and consumers stay consistent.
func FormatAddressDisplay(line1, line2, city, region, postcode, country string) string {
	parts := make([]string, 0, 5)
	if v := strings.TrimSpace(line1); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(line2); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(region); v != "" {
		parts = append(parts, v)
	}
	cityPart := strings.TrimSpace(strings.TrimSpace(postcode) + " " + strings.TrimSpace(city))
	if cityPart != "" {
		parts = append(parts, cityPart)
	}
	if v := strings.TrimSpace(country); v != "" {
		parts = append(parts, v)
	}
	return strings.Join(parts, ", ")
}

// FormatDisplay returns FormatAddressDisplay for a. Nil or empty postal content
// yields "".
func (a *Address) FormatDisplay() string {
	if a == nil || a.IsEmpty() {
		return ""
	}
	return FormatAddressDisplay(a.Line1, a.Line2, a.City, a.Region, a.Postcode, a.Country)
}

// FormatLines returns non-empty postal lines for invoice PDF seller blocks.
// Order: line1, line2, "postcode city", region, country.
func (a *Address) FormatLines() []string {
	if a == nil || a.IsEmpty() {
		return nil
	}
	lines := make([]string, 0, 5)
	if v := strings.TrimSpace(a.Line1); v != "" {
		lines = append(lines, v)
	}
	if v := strings.TrimSpace(a.Line2); v != "" {
		lines = append(lines, v)
	}
	cityPart := strings.TrimSpace(strings.TrimSpace(a.Postcode) + " " + strings.TrimSpace(a.City))
	if cityPart != "" {
		lines = append(lines, cityPart)
	}
	if v := strings.TrimSpace(a.Region); v != "" {
		lines = append(lines, v)
	}
	if v := strings.TrimSpace(a.Country); v != "" {
		lines = append(lines, v)
	}
	return lines
}

// FormatTaxIDLine returns "Label: value" (or just value when label is empty).
func FormatTaxIDLine(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return value
	}
	return label + ": " + value
}

// JoinTaxIDLines joins non-empty tax ID display lines with ", ".
func JoinTaxIDLines(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if v := strings.TrimSpace(line); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, ", ")
}
