package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const bundleComponentMaxQuantity = 9999

// BundleComponent links a bundle product to one of its component products.
type BundleComponent struct {
	BundleProductID    string
	ComponentProductID string
	Quantity           int
	Required           bool
	CreatedAt          time.Time
}

// NewBundleComponent builds a link with Required defaulting to true.
// IDs are trimmed; field validation runs before return.
func NewBundleComponent(bundleID, componentID string, quantity int) (*BundleComponent, error) {
	c := &BundleComponent{
		BundleProductID:    strings.TrimSpace(bundleID),
		ComponentProductID: strings.TrimSpace(componentID),
		Quantity:           quantity,
		Required:           true,
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// IsBundleComponentLeafType reports types allowed as bundle components (simple, virtual, downloadable).
func (t Type) IsBundleComponentLeafType() bool {
	switch t {
	case TypeSimple, TypeVirtual, TypeDownloadable:
		return true
	default:
		return false
	}
}

// Validate checks field-level constraints before persist.
// It does not trim IDs. NewBundleComponent and BundleService.Add trim before calling Validate.
func (c *BundleComponent) Validate() error {
	if c == nil {
		return errors.New("bundle component must not be nil")
	}
	bundleID := strings.TrimSpace(c.BundleProductID)
	componentID := strings.TrimSpace(c.ComponentProductID)
	if bundleID == "" {
		return errors.New("bundle product id must not be empty")
	}
	if componentID == "" {
		return errors.New("component product id must not be empty")
	}
	if c.Quantity < 1 {
		return errors.New("quantity must be at least 1")
	}
	if c.Quantity > bundleComponentMaxQuantity {
		return errors.New("quantity must not exceed 9999")
	}
	if bundleID == componentID {
		return errors.New("bundle product cannot reference itself as a component")
	}
	return nil
}

// ValidateComposition checks that the bundle and component product types allow
// this relationship. Call after loading both products from persistence.
func (c *BundleComponent) ValidateComposition(bundleType, componentType Type) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if bundleType != TypeBundle {
		return fmt.Errorf("only products with type %q can have bundle components", TypeBundle)
	}
	if !componentType.IsBundleComponentLeafType() {
		return fmt.Errorf("a bundle component product must have type simple, virtual, or downloadable")
	}
	return nil
}
