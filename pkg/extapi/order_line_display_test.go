package extapi_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestAppendOrderLineDisplayField(t *testing.T) {
	ctx := &extapi.HookContext{Name: string(extapi.HookOrderLineDisplay)}
	ctx.Set("order_id", "ord-1")
	ctx.Set("variant_id", "var-1")
	ctx.Set("extensions", map[string]interface{}{"design_id": "d1"})

	ctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{
		Code:  "plugin.thumb",
		Label: "Thumbnail",
		Kind:  extapi.DisplayKindImage,
		Value: "https://cdn.example/t.png",
		Alt:   "preview",
	})
	ctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{Code: ""}) // ignored

	if ctx.OrderLineDisplayOrderID() != "ord-1" || ctx.OrderLineDisplayVariantID() != "var-1" {
		t.Fatalf("ids = %q %q", ctx.OrderLineDisplayOrderID(), ctx.OrderLineDisplayVariantID())
	}
	if got := ctx.OrderLineDisplayExtensions()["design_id"]; got != "d1" {
		t.Fatalf("extensions = %#v", ctx.OrderLineDisplayExtensions())
	}
	raw, ok := ctx.Get("fields")
	if !ok {
		t.Fatal("fields missing")
	}
	fields := raw.(*[]extapi.OrderLineDisplayField)
	if len(*fields) != 1 || (*fields)[0].Code != "plugin.thumb" {
		t.Fatalf("fields = %+v", *fields)
	}
}
