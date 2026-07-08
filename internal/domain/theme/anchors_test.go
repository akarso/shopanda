package theme

import "testing"

func TestExtractDeclaredAnchors(t *testing.T) {
	source := `
{{slot_container "cart.summary"}}
<div></div>
{{/slot_container}}
{{slot . "layout.head" "append"}}
{{slot . "pdp.actions" "before"}}
`
	got := extractDeclaredAnchors(source)
	want := []string{"cart.summary", "layout.head", "pdp.actions"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
