package theme

import (
	"fmt"

	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/themefs"
)

// Load resolves inherited templates from disk and builds a theme engine.
func Load(dir string, opts ...domtheme.Option) (*domtheme.Engine, error) {
	resolved, meta, err := themefs.ResolveRootTheme(dir)
	if err != nil {
		return nil, fmt.Errorf("theme: load: %w", err)
	}
	return domtheme.NewEngine(resolved, meta, opts...)
}
