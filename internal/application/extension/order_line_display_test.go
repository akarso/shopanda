package extension_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

// publicImageLookup is the default test resolver: public A for ordinary hosts,
// loopback for nip.io / localdomain SSRF bait.
func publicImageLookup(_ context.Context, host string) ([]net.IP, error) {
	switch host {
	case "127.0.0.1.nip.io", "localhost.localdomain":
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	default:
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
}

func withPublicImageLookup(t *testing.T) {
	t.Helper()
	extensionapp.SetImageHostLookupForTest(t, publicImageLookup)
}

func TestNormalizeDisplayRow_ImageHTTPSOnly(t *testing.T) {
	withPublicImageLookup(t)

	okRow, ok := extensionapp.NormalizeDisplayRow(extensionapp.ExtensionDisplayRow{
		FieldCode: "thumb",
		Label:     "Design",
		Kind:      extensionapp.DisplayKindImage,
		Value:     "HTTPS://cdn.example/t.png",
		Alt:       "preview",
	})
	if !ok || okRow.Kind != extensionapp.DisplayKindImage || okRow.Alt != "preview" {
		t.Fatalf("HTTPS image = %+v ok=%v", okRow, ok)
	}
	if okRow.Value != "https://cdn.example/t.png" {
		t.Fatalf("scheme must normalize to https, got %q", okRow.Value)
	}
	for _, bad := range []string{
		"http://cdn.example/t.png",
		"javascript:alert(1)",
		"/relative.png",
		"https://user:pass@cdn.example/t.png",
		"https://127.0.0.1/t.png",
		"https://localhost/t.png",
		"https://localhost./t.png",
		"https://127.0.0.1.nip.io/t.png",
		"https://localhost.localdomain/t.png",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/t.png",
		"https://[::1]/t.png",
		"https://100.64.0.1/t.png",
		"https://192.0.2.1/t.png",
		"https://198.51.100.1/t.png",
		"https://203.0.113.1/t.png",
		"https://0.0.0.1/t.png",
		"https://[64:ff9b::7f00:1]/t.png",
		"https://[fe80::1%25eth0]/t.png",
		"https://127.1/",
		"https://2130706433/",
		"https://0x7f000001/",
		"https://0177.0.0.1/",
		"",
	} {
		if _, ok := extensionapp.NormalizeDisplayRow(extensionapp.ExtensionDisplayRow{
			FieldCode: "thumb",
			Kind:      extensionapp.DisplayKindImage,
			Value:     bad,
		}); ok {
			t.Fatalf("expected reject for %q", bad)
		}
	}
	if _, ok := extensionapp.NormalizeDisplayRow(extensionapp.ExtensionDisplayRow{
		FieldCode: "thumb",
		Kind:      "Image",
		Value:     "https://cdn.example/t.png",
	}); ok {
		t.Fatal("Kind Image (wrong case) must be rejected")
	}
}

func TestEnrichOrderLineDisplayRows_AppendsAndSkipsErrors(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "boom", func(ctx *hooks.Context) error {
		return errors.New("nope")
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "ok", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.thumb",
			Label: "Thumbnail",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/ok.png",
			Alt:   "thumb",
		})
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.bad",
			Kind:  extapi.DisplayKindImage,
			Value: "http://insecure.example/x.png",
		})
		return nil
	})

	var errs []string
	var skips []string
	base := []extensionapp.ExtensionDisplayRow{{
		FieldCode: "gift",
		Label:     "Gift",
		Kind:      extensionapp.DisplayKindText,
		Value:     "hi",
	}}
	rows := extensionapp.EnrichOrderLineDisplayRows(
		context.Background(),
		reg,
		"ord-1",
		"var-1",
		map[string]interface{}{"gift": "hi"},
		base,
		func(registrant string, err error) {
			errs = append(errs, registrant)
		},
		func(fieldCode, reason string) {
			skips = append(skips, fieldCode+":"+reason)
		},
	)
	if len(errs) != 1 || errs[0] != "boom" {
		t.Fatalf("errs = %v", errs)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want stored + https image (http dropped)", rows)
	}
	if rows[1].FieldCode != "plugin.thumb" || rows[1].Kind != extensionapp.DisplayKindImage {
		t.Fatalf("enriched = %+v", rows[1])
	}
	if len(skips) != 1 || !strings.Contains(skips[0], "plugin.bad") {
		t.Fatalf("skips = %v", skips)
	}
}

func TestEnrichOrderLineDisplayRows_DropsAppendsOnHandlerError(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "partial", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.gone",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/gone.png",
		})
		return errors.New("after append")
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "ok", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.keep",
			Kind:  extapi.DisplayKindText,
			Value: "ok",
		})
		return nil
	})

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", nil, nil, nil, nil)
	if len(rows) != 1 || rows[0].FieldCode != "plugin.keep" {
		t.Fatalf("rows = %+v, want only plugin.keep (partial appends dropped)", rows)
	}
}

func TestEnrichOrderLineDisplayRows_DropsInPlaceMutationOnError(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "seed", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code: "plugin.seed", Kind: extapi.DisplayKindImage, Value: "https://cdn.example/good.png",
		})
		return nil
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "mutate", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		(*fields)[0].Value = "https://evil.example/x.png"
		return errors.New("after mutate")
	})

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", nil, nil, nil, nil)
	if len(rows) != 1 || rows[0].Value != "https://cdn.example/good.png" {
		t.Fatalf("in-place mutate on error must roll back: %+v", rows)
	}
}

func TestEnrichOrderLineDisplayRows_DropsPointerReplaceOnError(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "seed", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code: "plugin.seed", Kind: extapi.DisplayKindText, Value: "keep",
		})
		return nil
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "replace", func(ctx *hooks.Context) error {
		short := []extapi.OrderLineDisplayField{{
			Code: "plugin.evil", Kind: extapi.DisplayKindImage, Value: "https://evil.example/x.png",
		}}
		ctx.Set("fields", &short)
		return errors.New("after replace")
	})

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", nil, nil, nil, nil)
	if len(rows) != 1 || rows[0].FieldCode != "plugin.seed" || rows[0].Value != "keep" {
		t.Fatalf("pointer replace on error must restore snapshot: %+v", rows)
	}
}

func TestEnrichOrderLineDisplayRows_DropsAppendsOnPanic(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "panic", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.panic",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/panic.png",
		})
		panic("boom")
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 200, "ok", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.after",
			Kind:  extapi.DisplayKindText,
			Value: "still",
		})
		return nil
	})

	var errs int
	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", nil, nil,
		func(string, error) { errs++ }, nil)
	if errs != 1 {
		t.Fatalf("errs = %d", errs)
	}
	if len(rows) != 1 || rows[0].FieldCode != "plugin.after" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestEnrichOrderLineDisplayRows_SealsOrderIDAgainstRewrite(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var seenBySecond string
	_ = reg.Register(hooks.HookOrderLineDisplay, 50, "attacker", func(ctx *hooks.Context) error {
		ctx.Set("order_id", "other-order")
		return nil
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		rawID, _ := ctx.Get("order_id")
		seenBySecond, _ = rawID.(string)
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.thumb",
			Kind:  extapi.DisplayKindText,
			Value: "id=" + seenBySecond,
		})
		return nil
	})

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-real", "var-1", nil, nil, nil, nil)
	if seenBySecond != "ord-real" {
		t.Fatalf("second handler saw order_id=%q, want ord-real", seenBySecond)
	}
	if len(rows) != 1 || rows[0].Value != "id=ord-real" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestEnrichOrderLineDisplayRows_DeepCopiesExtensionsPerHandler(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var seenBySecond interface{}
	_ = reg.Register(hooks.HookOrderLineDisplay, 50, "mutator", func(ctx *hooks.Context) error {
		rawExt, _ := ctx.Get("extensions")
		ext := rawExt.(map[string]interface{})
		ext["design_id"] = "tampered"
		nested := ext["meta"].(map[string]interface{})
		nested["color"] = "red"
		delete(ext, "gift")
		return nil
	})
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "reader", func(ctx *hooks.Context) error {
		rawExt, _ := ctx.Get("extensions")
		ext := rawExt.(map[string]interface{})
		seenBySecond = ext["design_id"]
		if _, ok := ext["gift"]; !ok {
			t.Fatal("second handler missing gift after first mutated")
		}
		nested := ext["meta"].(map[string]interface{})
		if nested["color"] != "blue" {
			t.Fatalf("nested color = %v, want blue (deep copy)", nested["color"])
		}
		return nil
	})

	original := map[string]interface{}{
		"gift":      "hi",
		"design_id": "d1",
		"meta":      map[string]interface{}{"color": "blue"},
	}
	hookExt := extensionapp.CloneExtensionMap(original)
	_ = extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", hookExt, nil, nil, nil)
	if seenBySecond != "d1" {
		t.Fatalf("second handler saw design_id=%v, want d1", seenBySecond)
	}
	if original["design_id"] != "d1" {
		t.Fatalf("caller original mutated: %#v", original)
	}
	if original["meta"].(map[string]interface{})["color"] != "blue" {
		t.Fatalf("nested original mutated: %#v", original["meta"])
	}
}

func TestEnrichOrderLineDisplayRows_NestedCloneIsolatesApplyOverrides(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "mutator", func(ctx *hooks.Context) error {
		rawExt, _ := ctx.Get("extensions")
		ext := rawExt.(map[string]interface{})
		ext["meta"].(map[string]interface{})["color"] = "red"
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code: "plugin.note", Kind: extapi.DisplayKindText, Value: "x",
		})
		return nil
	})

	original := map[string]interface{}{
		"meta": map[string]interface{}{"color": "blue"},
	}
	hookExt := extensionapp.CloneExtensionMap(original)
	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", hookExt, nil, nil, nil)
	extOut := extensionapp.ApplyEnrichedOverrides(extensionapp.CloneExtensionMap(original), rows, 0)
	if extOut["meta"].(map[string]interface{})["color"] != "blue" {
		t.Fatalf("ApplyEnrichedOverrides map saw hook mutation: %#v", extOut["meta"])
	}
}

func TestEnrichOrderLineDisplayRows_ExtensionMapIsolatedFromTemplate(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "mutator", func(ctx *hooks.Context) error {
		rawExt, _ := ctx.Get("extensions")
		ext, _ := rawExt.(map[string]interface{})
		ext["design_id"] = "tampered"
		delete(ext, "gift")
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.thumb",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/t.png",
			Alt:   "t",
		})
		return nil
	})

	original := map[string]interface{}{"gift": "hi", "design_id": "d1"}
	hookExt := extensionapp.CloneExtensionMap(original)
	base := []extensionapp.ExtensionDisplayRow{{
		FieldCode: "gift", Label: "Gift", Kind: extensionapp.DisplayKindText, Value: "hi",
	}}
	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1", hookExt, base, nil, nil)

	if original["design_id"] != "d1" || original["gift"] != "hi" {
		t.Fatalf("original map mutated: %#v", original)
	}
	extOut := extensionapp.ApplyEnrichedOverrides(extensionapp.CloneExtensionMap(original), rows, 1)
	if extOut["design_id"] != "d1" || extOut["gift"] != "hi" {
		t.Fatalf("template map = %#v", extOut)
	}
	if extOut["plugin.thumb"] != "https://cdn.example/t.png" {
		t.Fatalf("missing normalized enrich override: %#v", extOut)
	}
}

func TestEnrichOrderLineDisplayRows_OverwriteFromNormalizedNotLiveMap(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	_ = reg.Register(hooks.HookOrderLineDisplay, 100, "race", func(ctx *hooks.Context) error {
		rawExt, _ := ctx.Get("extensions")
		ext, _ := rawExt.(map[string]interface{})
		if ext == nil {
			ext = map[string]interface{}{}
			ctx.Set("extensions", ext)
		}
		ext["plugin.thumb"] = "http://evil.example/x.png"
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields, extapi.OrderLineDisplayField{
			Code:  "plugin.thumb",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/good.png",
			Alt:   "good",
		})
		return nil
	})

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-1",
		map[string]interface{}{}, nil, nil, nil)
	extOut := extensionapp.ApplyEnrichedOverrides(map[string]interface{}{}, rows, 0)
	if extOut["plugin.thumb"] != "https://cdn.example/good.png" {
		t.Fatalf("override must come from normalized row, got %#v", extOut)
	}
}

func TestEnrichOrderLineDisplayRows_PluginHooksRegisterPath(t *testing.T) {
	withPublicImageLookup(t)
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)
	if err := app.Hooks("metaldogs").Register(extapi.HookOrderLineDisplay, 100, func(ctx *extapi.HookContext) error {
		if ctx.OrderLineDisplayOrderID() != "ord-1" || ctx.OrderLineDisplayVariantID() != "var-9" {
			t.Fatalf("ids = %q %q", ctx.OrderLineDisplayOrderID(), ctx.OrderLineDisplayVariantID())
		}
		ctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{
			Code:  "plugin.thumb",
			Label: "Design",
			Kind:  extapi.DisplayKindImage,
			Value: "https://cdn.example/from-plugin.png",
			Alt:   "preview",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rows := extensionapp.EnrichOrderLineDisplayRows(context.Background(), reg, "ord-1", "var-9", nil, nil, nil, nil)
	if len(rows) != 1 || rows[0].Value != "https://cdn.example/from-plugin.png" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestDisplayRowsToTemplateList(t *testing.T) {
	list := extensionapp.DisplayRowsToTemplateList([]extensionapp.ExtensionDisplayRow{{
		FieldCode: "t",
		Label:     "L",
		Kind:      extensionapp.DisplayKindImage,
		Value:     "https://x/y",
		Alt:       "a",
	}})
	if len(list) != 1 || list[0]["Kind"] != "image" || list[0]["Alt"] != "a" {
		t.Fatalf("list = %+v", list)
	}
}
