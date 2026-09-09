package search

import "time"

// Scope describes which products a reindex run should cover, as requested
// by the caller. ReindexService.Trigger resolves it into a concrete run —
// see its own doc comment for the full-scan-threshold heuristic that may
// substitute ScopeAll for a requested partial scope.
//
// Scope is a closed set: the unexported method means only the four types
// below can implement it, the same "sealed interface" shape used for a
// fixed set of request kinds without reflection or a string enum.
type Scope interface {
	scope()
}

// ScopeAll requests a full-catalog reindex — the original, and still
// default, behavior (e.g. `search:reindex`'s CLI command).
type ScopeAll struct{}

func (ScopeAll) scope() {}

// ScopeProducts requests a reindex of exactly these product IDs.
type ScopeProducts struct {
	IDs []string
}

func (ScopeProducts) scope() {}

// ScopeCategories requests a reindex of every product assigned to any of
// these category IDs.
type ScopeCategories struct {
	IDs []string
}

func (ScopeCategories) scope() {}

// ScopeSince requests a reindex of every product whose updated_at is at or
// after Since.
type ScopeSince struct {
	Since time.Time
}

func (ScopeSince) scope() {}
