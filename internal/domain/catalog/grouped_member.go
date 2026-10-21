package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// GroupedMember is a display-only link from a grouped product to one member product.
// There is no quantity and no required flag: each member is a standalone product
// with its own price and its own order line.
//
// Membership does not affect the member's Purchasable result or visibility
// (PR-1055). A product that appears inside a group is exactly as purchasable
// and exactly as visible on its own page as it would be with no group row.
// Grouping never feeds ResolveVisibility or the purchasability gate.
//
// A member may belong to more than one group. The primary key is the pair,
// so the same member_product_id can appear under many group_product_id values.
type GroupedMember struct {
	GroupProductID  string
	MemberProductID string
	CreatedAt       time.Time
}

// NewGroupedMember builds a display link. IDs are trimmed; field validation runs before return.
func NewGroupedMember(groupID, memberID string) (*GroupedMember, error) {
	m := &GroupedMember{
		GroupProductID:  strings.TrimSpace(groupID),
		MemberProductID: strings.TrimSpace(memberID),
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// IsGroupedMemberLeafType reports types allowed as grouped members (simple, virtual, downloadable).
// This helper is separate from IsBundleComponentLeafType so the two composition rules
// can diverge later even while they currently allow the same set.
func (t Type) IsGroupedMemberLeafType() bool {
	switch t {
	case TypeSimple, TypeVirtual, TypeDownloadable:
		return true
	default:
		return false
	}
}

// Validate checks field-level constraints before persist.
// It does not trim IDs. NewGroupedMember and GroupedService.Add trim before calling Validate.
// It does not consult Purchasable or visibility; membership is not an input to those computations.
func (m *GroupedMember) Validate() error {
	if m == nil {
		return errors.New("grouped member must not be nil")
	}
	groupID := strings.TrimSpace(m.GroupProductID)
	memberID := strings.TrimSpace(m.MemberProductID)
	if groupID == "" {
		return errors.New("group product id must not be empty")
	}
	if memberID == "" {
		return errors.New("member product id must not be empty")
	}
	if groupID == memberID {
		return errors.New("grouped product cannot reference itself as a member")
	}
	return nil
}

// ValidateComposition checks that the group and member product types allow
// this display link. Call after loading both products from persistence.
// A passing result still does not change the member's Purchasable or visibility.
func (m *GroupedMember) ValidateComposition(groupType, memberType Type) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if groupType != TypeGrouped {
		return fmt.Errorf("only products with type %q can have grouped members", TypeGrouped)
	}
	if !memberType.IsGroupedMemberLeafType() {
		return fmt.Errorf("a grouped member product must have type simple, virtual, or downloadable")
	}
	return nil
}
