package theme

import (
	"strings"
	"testing"
)

func TestOrderedBlockNames_DeduplicatesTemplateOrder(t *testing.T) {
	got := OrderedBlockNames("pdp.info", LayoutConfig{}, []string{"meta", "meta", "actions"})
	want := []string{"meta", "actions"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestOverlayLayoutConfig_InheritsAndOverrides(t *testing.T) {
	base := LayoutConfig{
		Version: "1",
		Containers: map[string]ContainerBlocks{
			"pdp.info":   {Blocks: []string{"meta", "actions"}},
			"cart.items": {Blocks: []string{"lines"}},
		},
	}
	overlay := LayoutConfig{
		Version: "2",
		Containers: map[string]ContainerBlocks{
			"pdp.info": {Blocks: []string{"actions", "meta"}},
		},
	}

	got := OverlayLayoutConfig(base, overlay)

	if got.Version != "2" {
		t.Fatalf("version = %q, want 2", got.Version)
	}
	if blocks := got.Containers["pdp.info"].Blocks; len(blocks) != 2 || blocks[0] != "actions" {
		t.Fatalf("pdp.info = %v", blocks)
	}
	if blocks := got.Containers["cart.items"].Blocks; len(blocks) != 1 || blocks[0] != "lines" {
		t.Fatalf("cart.items = %v", blocks)
	}

	// base must remain unchanged
	if base.Containers["pdp.info"].Blocks[0] != "meta" {
		t.Fatal("overlay mutated base containers map")
	}
	if base.Version != "1" {
		t.Fatal("overlay mutated base version")
	}
}

func TestOverlayLayoutConfig_PreservesBaseVersionWhenOverlayEmpty(t *testing.T) {
	base := LayoutConfig{Version: "1", Containers: map[string]ContainerBlocks{"pdp.info": {Blocks: []string{"meta"}}}}
	got := OverlayLayoutConfig(base, LayoutConfig{Containers: map[string]ContainerBlocks{"pdp.info": {Blocks: []string{"actions"}}}})
	if got.Version != "1" {
		t.Fatalf("version = %q, want 1", got.Version)
	}
}

func TestPreprocessLayoutBlocks_NestedLayoutBlocks(t *testing.T) {
	source := `{{layout_blocks "outer"}}
{{layout_blocks "inner"}}
{{block "a"}}<div class="a">A</div>{{/block}}
{{block "b"}}<div class="b">B</div>{{/block}}
{{/layout_blocks}}
{{/layout_blocks}}`
	layout := LayoutConfig{
		Containers: map[string]ContainerBlocks{
			"inner": {Blocks: []string{"b", "a"}},
		},
	}

	got := preprocessLayoutBlocks(source, layout)
	if strings.Contains(got, "layout_blocks") || strings.Contains(got, "{{block") {
		t.Fatalf("expected markers stripped, got:\n%s", got)
	}
	b := strings.Index(got, `class="b"`)
	a := strings.Index(got, `class="a"`)
	if b < 0 || a < 0 || !(b < a) {
		t.Fatalf("expected inner b before a through nested containers, got:\n%s", got)
	}
}

func TestOrderedBlockNames_ConfigOverridesTemplateOrder(t *testing.T) {
	layout := LayoutConfig{
		Containers: map[string]ContainerBlocks{
			"pdp.info": {Blocks: []string{"actions", "meta", "description"}},
		},
	}
	got := OrderedBlockNames("pdp.info", layout, []string{"meta", "description", "actions"})
	want := []string{"actions", "meta", "description"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestOrderedBlockNames_AppendsUnknownBlocks(t *testing.T) {
	layout := LayoutConfig{
		Containers: map[string]ContainerBlocks{
			"pdp.info": {Blocks: []string{"actions", "missing"}},
		},
	}
	got := OrderedBlockNames("pdp.info", layout, []string{"meta", "actions"})
	want := []string{"actions", "meta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPreprocessLayoutBlocks_ReordersByConfig(t *testing.T) {
	source := `{{layout_blocks "pdp.info"}}
{{block "meta"}}<div class="meta">M</div>{{/block}}
{{block "actions"}}<div class="actions">A</div>{{/block}}
{{/layout_blocks}}`
	layout := LayoutConfig{
		Containers: map[string]ContainerBlocks{
			"pdp.info": {Blocks: []string{"actions", "meta"}},
		},
	}

	got := preprocessLayoutBlocks(source, layout)
	if strings.Contains(got, "layout_blocks") || strings.Contains(got, "{{block") {
		t.Fatalf("expected markers stripped, got:\n%s", got)
	}
	actions := strings.Index(got, `class="actions"`)
	meta := strings.Index(got, `class="meta"`)
	if actions < 0 || meta < 0 || !(actions < meta) {
		t.Fatalf("expected actions before meta, got:\n%s", got)
	}
}

func TestPreprocessLayoutBlocks_TemplateOrderWithoutConfig(t *testing.T) {
	source := `{{layout_blocks "pdp.info"}}
{{block "meta"}}<div class="meta">M</div>{{/block}}
{{block "actions"}}<div class="actions">A</div>{{/block}}
{{/layout_blocks}}`

	got := preprocessLayoutBlocks(source, LayoutConfig{})
	meta := strings.Index(got, `class="meta"`)
	actions := strings.Index(got, `class="actions"`)
	if meta < 0 || actions < 0 || !(meta < actions) {
		t.Fatalf("expected template order meta then actions, got:\n%s", got)
	}
}

func TestPreprocessTemplateSource_LayoutThenSlots(t *testing.T) {
	source := `{{layout_blocks "pdp.info"}}
{{block "meta"}}<div class="meta">{{.Name}}</div>{{/block}}
{{/layout_blocks}}
{{slot_container "pdp.info"}}
<div class="wrap">{{.Name}}</div>
{{/slot_container}}`
	layout := LayoutConfig{
		Containers: map[string]ContainerBlocks{
			"pdp.info": {Blocks: []string{"meta"}},
		},
	}

	got := preprocessTemplateSource(source, layout)
	if strings.Contains(got, "layout_blocks") || strings.Contains(got, "slot_container") {
		t.Fatalf("expected preprocess markers removed, got:\n%s", got)
	}
	if !strings.Contains(got, `{{slot . "pdp.info"`) {
		t.Fatalf("expected slot markers, got:\n%s", got)
	}
}
