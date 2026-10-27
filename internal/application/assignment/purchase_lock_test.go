package assignment

import (
	"reflect"
	"testing"
)

func TestAssignedPurchaseLockUnitIDs_CollectsParentAndChild(t *testing.T) {
	// RecordPurchase must lock the full batch before any line write so two
	// concurrent checkouts naming the same parents in opposite line order
	// cannot deadlock (LockAssignmentUnits sorts within one call only).
	got := assignedPurchaseLockUnitIDs([][2]string{
		{"order-a/card-1", "order-b/ticket-1"},
		{"order-c/card-2", "order-b/ticket-2"},
	})
	want := []string{
		"order-a/card-1", "order-b/ticket-1",
		"order-c/card-2", "order-b/ticket-2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assignedPurchaseLockUnitIDs = %#v, want %#v", got, want)
	}
}

func TestAssignedPurchaseLockUnitIDs_SkipsEmpty(t *testing.T) {
	got := assignedPurchaseLockUnitIDs([][2]string{
		{"", "child"},
		{"parent", ""},
		{"", ""},
	})
	want := []string{"child", "parent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assignedPurchaseLockUnitIDs = %#v, want %#v", got, want)
	}
}
