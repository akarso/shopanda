package extapi_test

import (
	"testing"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	slotsapp "github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestStableHookPointsMatchInternal(t *testing.T) {
	if string(extapi.HookCartAddItemAfter) != hooksapp.HookCartAddItemAfter {
		t.Fatalf("HookCartAddItemAfter = %q, want %q", extapi.HookCartAddItemAfter, hooksapp.HookCartAddItemAfter)
	}
}

func TestStableSlotAnchorsMatchInternalCatalog(t *testing.T) {
	internal := slotsapp.StandardAnchorNames()
	stable := extapi.SlotAnchorNames()
	if len(stable) != len(internal) {
		t.Fatalf("stable anchors len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("anchor[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestStablePlacementsMatchInternal(t *testing.T) {
	cases := []struct {
		stable extapi.Placement
		want   slotsapp.Placement
	}{
		{extapi.PlacementBefore, slotsapp.PlacementBefore},
		{extapi.PlacementAfter, slotsapp.PlacementAfter},
		{extapi.PlacementPrepend, slotsapp.PlacementPrepend},
		{extapi.PlacementAppend, slotsapp.PlacementAppend},
	}
	for _, tc := range cases {
		if string(tc.stable) != string(tc.want) {
			t.Fatalf("placement %q != %q", tc.stable, tc.want)
		}
	}
}
