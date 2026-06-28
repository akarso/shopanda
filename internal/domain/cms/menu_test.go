package cms_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/cms"
)

func TestValidMenuCode(t *testing.T) {
	if !cms.ValidMenuCode("header") || !cms.ValidMenuCode("footer") {
		t.Fatal("expected header and footer to be valid menu codes")
	}
	if cms.ValidMenuCode("sidebar") {
		t.Fatal("expected unknown code to be invalid")
	}
}

func TestNewMenuItemValidation(t *testing.T) {
	_, err := cms.NewMenuItem("item-1", "menu-1", "", "Home", cms.LinkTypeURL, "", 0)
	if err == nil {
		t.Fatal("expected url target required")
	}
	_, err = cms.NewMenuItem("item-1", "menu-1", "", "Home", cms.LinkTypeCategory, "", 0)
	if err == nil {
		t.Fatal("expected category target required")
	}
	item, err := cms.NewMenuItem("item-1", "menu-1", "", "Home", cms.LinkTypeURL, "/", 0)
	if err != nil || item.Label() != "Home" {
		t.Fatalf("unexpected item: %v err=%v", item, err)
	}
}

func TestValidateMenuItemsCycle(t *testing.T) {
	a, _ := cms.NewMenuItem("a", "menu-1", "", "A", cms.LinkTypeURL, "/", 0)
	b, _ := cms.NewMenuItem("b", "menu-1", "a", "B", cms.LinkTypeURL, "/b", 1)
	c, _ := cms.NewMenuItem("c", "menu-1", "b", "C", cms.LinkTypeURL, "/c", 2)
	aCycle := cms.NewMenuItemFromDB("a", "menu-1", "c", "A", cms.LinkTypeURL, "/", 0, true, a.CreatedAt(), a.UpdatedAt())
	if err := cms.ValidateMenuItems([]*cms.MenuItem{aCycle, b, c}); err == nil {
		t.Fatal("expected cycle error")
	}
	if err := cms.ValidateMenuItems([]*cms.MenuItem{a, b, c}); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
}

func TestValidateMenuItemsDuplicateID(t *testing.T) {
	a, _ := cms.NewMenuItem("dup", "menu-1", "", "A", cms.LinkTypeURL, "/", 0)
	b, _ := cms.NewMenuItem("dup", "menu-1", "", "B", cms.LinkTypeURL, "/b", 1)
	if err := cms.ValidateMenuItems([]*cms.MenuItem{a, b}); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestValidateMenuItemsUnknownParent(t *testing.T) {
	item, _ := cms.NewMenuItem("a", "menu-1", "missing", "A", cms.LinkTypeURL, "/", 0)
	if err := cms.ValidateMenuItems([]*cms.MenuItem{item}); err == nil {
		t.Fatal("expected unknown parent error")
	}
}
