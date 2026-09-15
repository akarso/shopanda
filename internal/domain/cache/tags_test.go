package cache

import (
	"reflect"
	"testing"
)

func TestUniqueTags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []string{}, want: nil},
		{name: "passthrough", in: []string{"cms:7", "cat:12"}, want: []string{"cms:7", "cat:12"}},
		{name: "drops empty and whitespace", in: []string{"", "  ", "\t", "cms:7"}, want: []string{"cms:7"}},
		{name: "trims", in: []string{"  cms:7  ", "cat:12"}, want: []string{"cms:7", "cat:12"}},
		{name: "dedupes first-seen", in: []string{"a", "b", "a", "b"}, want: []string{"a", "b"}},
		{name: "case sensitive", in: []string{"CMS:7", "cms:7"}, want: []string{"CMS:7", "cms:7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UniqueTags(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("UniqueTags(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"\tcms:7\n", "cms:7"},
		{"cms:7", "cms:7"},
	}
	for _, tc := range cases {
		if got := NormalizeTag(tc.in); got != tc.want {
			t.Fatalf("NormalizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
