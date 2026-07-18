package exportctx

import (
	"fmt"
	"regexp"
	"strings"
)

// Supported export entities (stable catalog; mirrors import entities).
const (
	EntityProduct   = "product"
	EntityPrice     = "price"
	EntityStock     = "stock"
	EntityCategory  = "category"
	EntityCustomer  = "customer"
	EntityAttribute = "attribute"
)

var supportedEntities = map[string]struct{}{
	EntityProduct:   {},
	EntityPrice:     {},
	EntityStock:     {},
	EntityCategory:  {},
	EntityCustomer:  {},
	EntityAttribute: {},
}

// EntityCatalog returns supported export entity names in stable order.
func EntityCatalog() []string {
	return []string{
		EntityProduct,
		EntityPrice,
		EntityStock,
		EntityCategory,
		EntityCustomer,
		EntityAttribute,
	}
}

var rowHookNamePattern = regexp.MustCompile(`^export\.[a-z][a-z0-9_]*\.row$`)

// RowHookName returns the hook point name for an export entity.
func RowHookName(entity string) (string, error) {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return "", fmt.Errorf("export entity must not be empty")
	}
	if _, ok := supportedEntities[entity]; !ok {
		return "", fmt.Errorf("export entity %q: unsupported (catalog: %s)", entity, strings.Join(EntityCatalog(), ", "))
	}
	name := "export." + entity + ".row"
	if !rowHookNamePattern.MatchString(name) {
		return "", fmt.Errorf("export row hook %q: invalid format", name)
	}
	return name, nil
}

// ValidateEntity checks whether entity is in the supported catalog.
func ValidateEntity(entity string) error {
	_, err := RowHookName(entity)
	return err
}
