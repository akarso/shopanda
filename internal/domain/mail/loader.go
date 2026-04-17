package mail

// TemplateLoader resolves an email template by name.
// Implementations decide the lookup strategy (file, DB, built-in, etc.).
type TemplateLoader interface {
	// Load returns the raw subject and body strings for the named template.
	// The body is the inner HTML before layout wrapping.
	// Returns ErrTemplateNotFound when the template does not exist.
	Load(name string) (subject, body string, err error)
}

// EmailData is the common data structure passed to every email template.
// Templates access top-level fields directly (e.g. {{.StoreName}}) and
// template-specific values via the Data map (e.g. {{.Data.OrderID}}).
type EmailData struct {
	StoreName    string
	StoreURL     string
	LogoURL      string
	StoreAddress string

	// Template-specific data.
	Data map[string]interface{}
}
