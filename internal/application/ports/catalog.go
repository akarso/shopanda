package ports

// CatalogEntry describes a replaceable infrastructure port (documented contract).
type CatalogEntry struct {
	Name        string
	RegisterAPI string
	ConfigKey   string
	Notes       string
	Shipped     bool
}

// Catalog returns the canonical infrastructure port catalog for introspection.
// Order is stable for API responses and CLI output.
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{
			Name:        "search",
			RegisterAPI: "RegisterSearchProvider",
			ConfigKey:   "search.engine",
			Notes:       "Product search backend (postgres, meilisearch)",
			Shipped:     true,
		},
		{
			Name:        "cache",
			RegisterAPI: "RegisterCache",
			ConfigKey:   "cache.driver",
			Notes:       "Application cache store (postgres, redis)",
			Shipped:     true,
		},
		{
			Name:        "queue",
			RegisterAPI: "RegisterQueue",
			ConfigKey:   "queue.driver",
			Notes:       "Background job queue (postgres, redis, rabbitmq, kafka, sqs)",
			Shipped:     true,
		},
		{
			Name:        "payment",
			RegisterAPI: "RegisterPaymentProvider",
			ConfigKey:   "payment.providers",
			Notes:       "Payment providers by method (manual, stripe, …); multiple may register",
			Shipped:     true,
		},
		{
			Name:        "media",
			RegisterAPI: "RegisterMediaStorage",
			ConfigKey:   "media.storage",
			Notes:       "Media object storage (local, s3)",
			Shipped:     true,
		},
		{
			Name:        "tax",
			RegisterAPI: "RegisterTaxCalculator",
			ConfigKey:   "",
			Notes:       "Tax calculation; core default uses rate tables",
			Shipped:     true,
		},
		{
			Name:        "shipping_rate",
			RegisterAPI: "RegisterShippingRateProvider",
			ConfigKey:   "",
			Notes:       "Carrier rate lookup; zone tables remain core",
			Shipped:     true,
		},
		{
			Name:        "mail_sender",
			RegisterAPI: "RegisterMailSender",
			ConfigKey:   "mail.driver",
			Notes:       "Transactional mail delivery",
			Shipped:     true,
		},
		{
			Name:        "address_validator",
			RegisterAPI: "RegisterAddressValidator",
			ConfigKey:   "",
			Notes:       "Optional address validation against external services (backlog)",
			Shipped:     false,
		},
	}
}
