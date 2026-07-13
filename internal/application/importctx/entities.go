package importctx

import (
	"fmt"
	"regexp"
	"strings"
)

// Supported import entities (stable catalog).
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

// EntityCatalog returns supported import entity names in stable order.
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

var rowHookNamePattern = regexp.MustCompile(`^import\.[a-z][a-z0-9_]*\.row$`)

// RowHookName returns the hook point name for an import entity.
func RowHookName(entity string) (string, error) {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return "", fmt.Errorf("import entity must not be empty")
	}
	if _, ok := supportedEntities[entity]; !ok {
		return "", fmt.Errorf("import entity %q: unsupported (catalog: %s)", entity, strings.Join(EntityCatalog(), ", "))
	}
	name := "import." + entity + ".row"
	if !rowHookNamePattern.MatchString(name) {
		return "", fmt.Errorf("import row hook %q: invalid format", name)
	}
	return name, nil
}

// ValidateEntity checks whether entity is in the supported catalog.
func ValidateEntity(entity string) error {
	_, err := RowHookName(entity)
	return err
}
