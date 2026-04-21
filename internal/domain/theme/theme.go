package theme

type StorefrontNavLink struct {
	Label string `yaml:"label"`
	URL   string `yaml:"url"`
}

type StorefrontConfig struct {
	SearchAction string              `yaml:"search_action"`
	CartURL      string              `yaml:"cart_url"`
	CartLabel    string              `yaml:"cart_label"`
	Nav          []StorefrontNavLink `yaml:"nav"`
}

// Theme holds metadata loaded from theme.yaml.
type Theme struct {
	Name       string           `yaml:"name"`
	Version    string           `yaml:"version"`
	Storefront StorefrontConfig `yaml:"storefront"`
}
