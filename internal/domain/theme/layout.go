package theme

// LayoutConfig holds declarative block order for fixed template containers.
type LayoutConfig struct {
	Version    string                     `yaml:"version"`
	Containers map[string]ContainerBlocks `yaml:"containers"`
}

// ContainerBlocks lists block names in render order for one container.
type ContainerBlocks struct {
	Blocks []string `yaml:"blocks"`
}

// OverlayLayoutConfig overlays child container order onto parent (child wins per container).
func OverlayLayoutConfig(base, overlay LayoutConfig) LayoutConfig {
	merged := base
	if merged.Containers == nil {
		merged.Containers = make(map[string]ContainerBlocks)
	}
	for name, blocks := range overlay.Containers {
		merged.Containers[name] = blocks
	}
	if overlay.Version != "" {
		merged.Version = overlay.Version
	}
	return merged
}

// OrderedBlockNames returns block names for container: config order first, then template order.
func OrderedBlockNames(container string, layout LayoutConfig, templateOrder []string) []string {
	seen := make(map[string]struct{}, len(templateOrder))
	for _, name := range templateOrder {
		seen[name] = struct{}{}
	}

	var out []string
	added := make(map[string]struct{})
	if cfg, ok := layout.Containers[container]; ok {
		for _, name := range cfg.Blocks {
			if _, ok := seen[name]; !ok {
				continue
			}
			if _, dup := added[name]; dup {
				continue
			}
			added[name] = struct{}{}
			out = append(out, name)
		}
	}
	for _, name := range templateOrder {
		if _, ok := added[name]; ok {
			continue
		}
		out = append(out, name)
	}
	return out
}
