package theme

// Theme holds metadata loaded from theme.yaml.
type Theme struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}
