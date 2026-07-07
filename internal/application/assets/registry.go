package assets

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type assetKey struct {
	registrant string
	path       string
	placement  Placement
	kind       Kind
}

type registeredAsset struct {
	key      assetKey
	priority int
	manifest Manifest
}

// Registry stores plugin asset manifests and resolves them per route.
type Registry struct {
	mu      sync.RWMutex
	entries []registeredAsset
}

// NewRegistry creates an empty asset registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a manifest for registrant. Re-registering the same
// (registrant, path, placement, kind) is a no-op for dev reload safety.
func (r *Registry) Register(registrant string, manifest Manifest) error {
	if r == nil {
		return fmt.Errorf("assets: registry must not be nil")
	}
	registrant = strings.TrimSpace(registrant)
	if registrant == "" {
		return fmt.Errorf("assets: registrant must not be empty")
	}
	manifest.Path = strings.TrimSpace(manifest.Path)
	if err := manifest.Validate(); err != nil {
		return err
	}

	key := assetKey{
		registrant: registrant,
		path:       manifest.Path,
		placement:  manifest.Placement,
		kind:       manifest.Kind,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		if entry.key == key {
			return nil
		}
	}
	r.entries = append(r.entries, registeredAsset{
		key:      key,
		priority: manifest.Priority,
		manifest: manifest,
	})
	sort.SliceStable(r.entries, func(i, j int) bool {
		a, b := r.entries[i], r.entries[j]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if a.key.registrant != b.key.registrant {
			return a.key.registrant < b.key.registrant
		}
		return a.key.path < b.key.path
	})
	return nil
}

// ForRoute returns assets that should be injected for path.
func (r *Registry) ForRoute(path string) Bundle {
	if r == nil {
		return Bundle{}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return Bundle{}
	}

	r.mu.RLock()
	entries := append([]registeredAsset(nil), r.entries...)
	r.mu.RUnlock()

	var bundle Bundle
	for _, entry := range entries {
		if !manifestMatchesRoute(entry.manifest.Routes, path) {
			continue
		}
		resolved := Resolved{URL: entry.manifest.Path}
		switch entry.manifest.Placement {
		case PlacementHead:
			switch entry.manifest.Kind {
			case KindCSS:
				bundle.HeadCSS = append(bundle.HeadCSS, resolved)
			case KindJS:
				bundle.HeadJS = append(bundle.HeadJS, resolved)
			}
		case PlacementFooter:
			if entry.manifest.Kind == KindJS {
				bundle.FooterJS = append(bundle.FooterJS, resolved)
			}
		}
	}
	return bundle
}
