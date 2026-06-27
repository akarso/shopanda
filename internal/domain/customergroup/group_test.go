package customergroup_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewGroup_Success(t *testing.T) {
	g, err := customergroup.NewGroup(id.New(), "wholesale", "Wholesale buyers", "Tier 1")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if g.Code != "wholesale" {
		t.Fatalf("Code = %q", g.Code)
	}
}

func TestNewGroup_InvalidID(t *testing.T) {
	_, err := customergroup.NewGroup("not-a-uuid", "wholesale", "Name", "")
	if err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestNewGroup_InvalidCode(t *testing.T) {
	_, err := customergroup.NewGroup(id.New(), "Bad Code", "Name", "")
	if err == nil {
		t.Fatal("expected invalid code error")
	}
}

func TestGroup_Update(t *testing.T) {
	g, err := customergroup.NewGroup(id.New(), "vip", "VIP", "")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if err := g.Update("VIP customers", "Updated"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if g.Name != "VIP customers" {
		t.Fatalf("Name = %q", g.Name)
	}
}
