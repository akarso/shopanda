package extapi

// FieldType is the typed payload kind for an extension field.
type FieldType string

const (
	FieldTypeString   FieldType = "string"
	FieldTypeInt      FieldType = "int"
	FieldTypeBool     FieldType = "bool"
	FieldTypeEnum     FieldType = "enum"
	FieldTypeJSON     FieldType = "json"
	FieldTypeMoney    FieldType = "money"
	FieldTypeDate     FieldType = "date"
	FieldTypeDateTime FieldType = "datetime"
)

// TargetType identifies where an extension field applies.
type TargetType string

const (
	TargetProduct      TargetType = "product"
	TargetVariant      TargetType = "variant"
	TargetCartItem     TargetType = "cart_item"
	TargetOrderItem    TargetType = "order_item"
	TargetCustomer     TargetType = "customer"
	TargetPDP          TargetType = "pdp"
	TargetPLPItem      TargetType = "plp_item"
	TargetCartView     TargetType = "cart_view"
	TargetCheckoutView TargetType = "checkout_view"
)

// StorageMode declares how field values are persisted and propagated.
type StorageMode string

const (
	StorageStored   StorageMode = "stored"
	StorageComputed StorageMode = "computed"
	StorageSnapshot StorageMode = "snapshot"
)

// Visibility controls default admin and API exposure.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// Access declares role-based read/write constraints.
//
// PluginOnlyWrite: values may be written only via Host.WithPluginWrite /
// PluginWriteContext naming the field code. Public/admin/GraphQL writes get 403.
// Trusted-write trust is stripped at bus Publish, pricing Pipeline, checkout
// Workflow, and jobs Worker — and for cart hooks/pricing (retained only for
// extension upsert). See AGENTS extension recipes.
type Access struct {
	ReadRoles       []string
	WriteRoles      []string
	PluginOnlyWrite bool
}

// Validation holds static constraints applied at write time.
type Validation struct {
	Required bool
	Min      *int64
	Max      *int64
	Regex    string
	Options  []string
}

// FieldDef is the input shape for registering an extension field via Host.
type FieldDef struct {
	Code        string
	Label       string
	Description string
	Type        FieldType
	Scope       TargetType
	StorageMode StorageMode
	Visibility  Visibility
	Access      Access
	Validation  Validation
}
