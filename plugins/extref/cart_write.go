package extref

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// AddItemWithGiftNote adds a line and sets the plugin-only gift note via CartPort (trusted write).
func AddItemWithGiftNote(ctx context.Context, h pluginsdk.Host, cartID, customerID, variantID string, qty int, note string) error {
	if h == nil {
		return fmt.Errorf("extref: host not configured")
	}
	port, ok := h.Cart()
	if !ok || port == nil {
		return fmt.Errorf("extref: cart port not bound")
	}
	ctx = h.WithPluginWrite(ctx, PluginName, FieldCodeGiftNote)
	return port.AddItem(ctx, cartID, customerID, variantID, qty, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{Code: FieldCodeGiftNote, Value: note}},
		UpdatedBy:  "plugin:" + PluginName,
	})
}
