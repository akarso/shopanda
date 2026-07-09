package slotsdemo

import "github.com/akarso/shopanda/pkg/extapi"

const (
	markerLayoutFooter = `data-slotsdemo="layout-footer"`
	markerPDPInfo      = `data-slotsdemo="pdp-info"`
)

func renderLayoutFooter(_ *extapi.SlotRenderContext) string {
	return `<span ` + markerLayoutFooter + ` class="slotsdemo-layout-footer">Slots demo: layout footer</span>`
}

func renderPDPInfo(_ *extapi.SlotRenderContext) string {
	return `<span ` + markerPDPInfo + ` class="slotsdemo-pdp-info">Slots demo: PDP info</span>`
}
