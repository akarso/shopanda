package cache

import "strings"

// UniqueTags returns tags with leading/trailing whitespace stripped, empty
// names dropped, and duplicates removed, preserving first-seen order.
// Tags are case-sensitive opaque strings ("CMS:7" and "cms:7" are distinct).
func UniqueTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}
