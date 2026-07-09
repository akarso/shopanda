package theme

import (
	"strings"
	"testing"
)

func TestSlotContainerOpenPattern_NoFalsePositiveOnDefine(t *testing.T) {
	source := `{{ define "title" }}Slots{{ end }}
{{ define "content" }}
{{slot_container "pdp.price"}}`
	match := slotContainerOpenPattern.FindStringSubmatchIndex(source)
	if match == nil {
		t.Fatal("expected real match")
	}
	got := source[match[0]:match[1]]
	if got != `{{slot_container "pdp.price"}}` {
		t.Fatalf("match = %q at %d", got, match[0])
	}
}

func TestFindSlotContainerOpen_PageTemplate(t *testing.T) {
	source := `{{ define "title" }}Slots{{ end }}
{{ define "content" }}
{{slot_container "pdp.price"}}
<div class="product-price">{{.Price}}</div>
{{/slot_container}}
{{ end }}
{{ template "layout.html" . }}`

	anchor, openStart, openEnd, ok := findSlotContainerOpen(source, 0)
	if !ok {
		t.Fatal("expected open")
	}
	if anchor != "pdp.price" {
		t.Fatalf("anchor = %q", anchor)
	}
	if openStart != strings.Index(source, `{{slot_container "pdp.price"}}`) {
		t.Fatalf("openStart = %d, want %d", openStart, strings.Index(source, `{{slot_container "pdp.price"}}`))
	}
	closeStart, closeEnd, ok := findMatchingSlotContainerClose(source, openEnd)
	if !ok {
		t.Fatalf("expected close after open at %d", openEnd)
	}
	if source[closeStart:closeEnd] != slotContainerCloseTag {
		t.Fatalf("close = %q", source[closeStart:closeEnd])
	}
}

func TestPreprocessSlotContainers_PageTemplateWithDefines(t *testing.T) {
	source := `{{ define "title" }}Slots{{ end }}
{{ define "content" }}
{{slot_container "pdp.price"}}
<div class="product-price">{{.Price}}</div>
{{/slot_container}}
{{ end }}
{{ template "layout.html" . }}`

	got := preprocessSlotContainers(source)
	if strings.Contains(got, "slot_container") {
		t.Fatalf("expected expansion in page template, got:\n%s", got)
	}
}

func TestPreprocessSlotContainers_SimpleWithTemplateExpr(t *testing.T) {
	source := `{{slot_container "pdp.price"}}
<div class="product-price">{{.Price}}</div>
{{/slot_container}}`

	got := preprocessSlotContainers(source)
	if strings.Contains(got, "slot_container") {
		t.Fatalf("expected expansion, got:\n%s", got)
	}
	if !strings.Contains(got, `{{.Price}}`) {
		t.Fatalf("template expression should remain:\n%s", got)
	}
}

func TestPreprocessSlotContainers_NestedContainers(t *testing.T) {
	t.Run("inner immediately after outer open", func(t *testing.T) {
		source := `{{slot_container "outer"}}
{{slot_container "inner"}}
<div class="inner">x</div>
{{/slot_container}}
<p>tail</p>
{{/slot_container}}`
		assertNestedPreprocess(t, source)
	})

	t.Run("template actions before inner container", func(t *testing.T) {
		source := `{{slot_container "outer"}}
<div class="price">{{.Price}}</div>
{{if .Show}}
{{slot_container "inner"}}
<div class="inner">x</div>
{{/slot_container}}
{{end}}
<p>tail</p>
{{/slot_container}}`
		got := assertNestedPreprocess(t, source)
		if !strings.Contains(got, `{{.Price}}`) || !strings.Contains(got, `{{if .Show}}`) {
			t.Fatalf("template actions should remain:\n%s", got)
		}
	})
}

func assertNestedPreprocess(t *testing.T, source string) string {
	t.Helper()

	got := preprocessSlotContainers(source)

	for _, anchor := range []string{"outer", "inner"} {
		for _, placement := range []string{"before", "prepend", "append", "after"} {
			marker := `{{slot . "` + anchor + `" "` + placement + `"}}`
			if !strings.Contains(got, marker) {
				t.Fatalf("missing %s in:\n%s", marker, got)
			}
		}
	}

	innerBefore := strings.Index(got, `{{slot . "inner" "before"}}`)
	innerAfter := strings.Index(got, `{{slot . "inner" "after"}}`)
	outerBefore := strings.Index(got, `{{slot . "outer" "before"}}`)
	outerAfter := strings.Index(got, `{{slot . "outer" "after"}}`)
	if !(outerBefore < innerBefore && innerAfter < outerAfter) {
		t.Fatalf("nested expansion order wrong:\n%s", got)
	}
	return got
}

func TestPreprocessSlotContainers_SiblingContainers(t *testing.T) {
	source := `{{slot_container "first"}}
<div>a</div>
{{/slot_container}}
{{slot_container "second"}}
<div>b</div>
{{/slot_container}}`

	got := preprocessSlotContainers(source)
	if !strings.Contains(got, `{{slot . "first" "before"}}`) || !strings.Contains(got, `{{slot . "second" "before"}}`) {
		t.Fatalf("expected both siblings expanded:\n%s", got)
	}
	firstAfter := strings.Index(got, `{{slot . "first" "after"}}`)
	secondBefore := strings.Index(got, `{{slot . "second" "before"}}`)
	if firstAfter < 0 || secondBefore < 0 || !(firstAfter < secondBefore) {
		t.Fatalf("sibling order wrong:\n%s", got)
	}
}

func TestPreprocessSlotContainers_UnclosedLeavesSource(t *testing.T) {
	source := `{{slot_container "broken"}}
<div>stay</div>`
	if got := preprocessSlotContainers(source); got != source {
		t.Fatalf("unclosed container should be unchanged, got:\n%s", got)
	}
}
