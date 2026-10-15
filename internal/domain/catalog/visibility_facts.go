package catalog

import (
	"context"
	"errors"
)

// ErrVisibilityScanBudget is retained for callers that still check it.
// ListVisiblePage no longer returns it: budget exhaustion yields a partial
// page with Truncated=true and NextAfter set for resume (PR-1057).
var ErrVisibilityScanBudget = errors.New("catalog: visibility scan budget exhausted before page filled")

// VisibilitySnapshot is status + modes + AutoBasis facts for one product
// (PR-1057). Enough to resolve axes without loading the full Product row.
type VisibilitySnapshot struct {
	Status Status
	Modes  VisibilityAxes
	Inputs VisibilityInputs
}

// Resolve applies modes and inputs under opts.
func (s VisibilitySnapshot) Resolve(opts VisibilityOptions) Visibility {
	return s.Modes.Resolve(s.Status, s.Inputs, opts)
}

// VisibilityFactsSource loads visibility snapshots for products (PR-1057).
// Missing IDs are omitted — callers treat missing as not visible when the
// gate is active (facts non-nil).
type VisibilityFactsSource interface {
	LoadMany(ctx context.Context, productIDs []string) (map[string]VisibilitySnapshot, error)
}

// AllowWithFacts resolves the surface gate using a loaded snapshot.
// Nil facts means the gate is not configured (pass). A missing snapshot
// denies (fail-closed). Prefer the DB snapshot over the in-memory Product
// row when both are present.
func AllowWithFacts(
	ctx context.Context,
	p *Product,
	surface VisibilitySurface,
	facts VisibilityFactsSource,
	opts VisibilityOptions,
) (bool, error) {
	if p == nil {
		return false, nil
	}
	if BypassProductVisibility(ctx) {
		return true, nil
	}
	if facts == nil {
		return true, nil
	}
	m, err := facts.LoadMany(ctx, []string{p.ID})
	if err != nil {
		return false, err
	}
	snap, ok := m[p.ID]
	if !ok {
		return false, nil
	}
	return snap.Resolve(opts).AllowedOn(surface), nil
}

// FilterWithFacts keeps products allowed on surface. Nil facts passes all
// through (gate off). Missing snapshots are denied.
func FilterWithFacts(
	ctx context.Context,
	products []Product,
	surface VisibilitySurface,
	facts VisibilityFactsSource,
	opts VisibilityOptions,
) ([]Product, error) {
	if len(products) == 0 {
		return []Product{}, nil
	}
	if BypassProductVisibility(ctx) {
		return append([]Product(nil), products...), nil
	}
	if facts == nil {
		return append([]Product(nil), products...), nil
	}
	ids := make([]string, len(products))
	for i := range products {
		ids[i] = products[i].ID
	}
	byID, err := facts.LoadMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Product, 0, len(products))
	for i := range products {
		snap, ok := byID[products[i].ID]
		if !ok {
			continue
		}
		if snap.Resolve(opts).AllowedOn(surface) {
			out = append(out, products[i])
		}
	}
	return out, nil
}

// FilterIDsWithFacts returns the subset of productIDs allowed on surface,
// preserving order. Used for search hits and carousel ID lists (PR-1057).
// Temporary query-time filter until the search index carries visibility
// (PR-1079). Nil facts passes all IDs through. Empty input returns a
// non-nil empty slice.
func FilterIDsWithFacts(
	ctx context.Context,
	productIDs []string,
	surface VisibilitySurface,
	facts VisibilityFactsSource,
	opts VisibilityOptions,
) ([]string, error) {
	if len(productIDs) == 0 {
		return []string{}, nil
	}
	if BypassProductVisibility(ctx) {
		return append([]string(nil), productIDs...), nil
	}
	if facts == nil {
		return append([]string(nil), productIDs...), nil
	}
	byID, err := facts.LoadMany(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(productIDs))
	for _, id := range productIDs {
		snap, ok := byID[id]
		if !ok {
			continue
		}
		if snap.Resolve(opts).AllowedOn(surface) {
			out = append(out, id)
		}
	}
	return out, nil
}

const (
	visibilityScanHardCap     = 10000
	visibilityScanMinHeadroom = 2000 // sparse AutoBasis catalogs (mostly OOS)
	// visibilityScanMaxBatch matches postgres ProductRepo maxListLimit (100).
	// Requesting more silently returns a short page that must not be treated
	// as end-of-catalog.
	visibilityScanMaxBatch = 100
)

// VisiblePage is one page of axis-filtered products (PR-1057).
type VisiblePage struct {
	Products []Product
	// Truncated is true when the scan budget stopped before Limit was
	// filled (or before VisibleSkip was satisfied). The page may be short
	// or empty; continue with NextAfter when HasMore — do not advance offset.
	Truncated bool
	// HasMore is true when more repository rows may exist at NextAfter.
	// The next page may still be empty (trailing hidden-only rows).
	HasMore bool
	// DepthLimited is true when cursor resume would exceed
	// MaxListCursorOffset. HasMore is false and NextAfter is empty; classic
	// ?offset= may still reach deeper rows until keyset pagination.
	DepthLimited bool
	// NextAfter is an opaque resume cursor (NextAfterCursor). Empty when
	// !HasMore or DepthLimited.
	NextAfter string
}

// ListVisiblePageParams controls a visible listing walk.
type ListVisiblePageParams struct {
	// AfterSet means AfterOffset/AfterSkip came from a decoded resume cursor.
	// When true, VisibleSkip is ignored.
	AfterSet    bool
	AfterOffset int
	AfterSkip   int
	// VisibleSkip skips this many visible products from the start of the
	// scan (classic offset). Only applied when AfterSet is false.
	VisibleSkip int
	Limit       int
	// MaxScan caps repository rows read. <= 0 selects an automatic budget
	// based on Limit + remaining skip, capped at hardCap.
	MaxScan int
}

// ListVisiblePage walks listFn in repository order and collects visible
// products. Multi-page walks must use NextAfter so remaining VisibleSkip
// survives budget stops and scans resume mid-table. Never emits a
// NextAfter that DecodeListCursor would reject (depth cap).
func ListVisiblePage(
	ctx context.Context,
	listFn func(ctx context.Context, dbOffset, dbLimit int) ([]Product, error),
	surface VisibilitySurface,
	facts VisibilityFactsSource,
	opts VisibilityOptions,
	params ListVisiblePageParams,
) (VisiblePage, error) {
	if params.Limit <= 0 {
		return VisiblePage{}, nil
	}
	if params.VisibleSkip < 0 {
		params.VisibleSkip = 0
	}

	dbOffset := 0
	visibleSkip := params.VisibleSkip
	if params.AfterSet {
		dbOffset = params.AfterOffset
		visibleSkip = params.AfterSkip
	}

	gateOff := BypassProductVisibility(ctx) || facts == nil
	if gateOff {
		// Every row is "visible"; remaining skip collapses into OFFSET.
		off := dbOffset + visibleSkip
		page, err := listFn(ctx, off, params.Limit)
		if err != nil {
			return VisiblePage{}, err
		}
		hasMore := len(page) == params.Limit
		next := ""
		if hasMore {
			next = NextAfterCursor(off+len(page), 0)
			if next == "" {
				// Depth limit: do not hand out a cursor we would reject.
				return VisiblePage{Products: page, DepthLimited: true}, nil
			}
		}
		return VisiblePage{Products: page, HasMore: hasMore, NextAfter: next}, nil
	}

	needed := visibleSkip + params.Limit
	maxScan := params.MaxScan
	if maxScan <= 0 {
		maxScan = needed + visibilityScanMinHeadroom
	}
	if maxScan < params.Limit {
		maxScan = params.Limit
	}
	if maxScan > visibilityScanHardCap {
		maxScan = visibilityScanHardCap
	}

	batchSize := visibilityScanMaxBatch

	skipped := 0
	scanned := 0
	out := make([]Product, 0, params.Limit)
	resumeAt := dbOffset

	for len(out) < params.Limit && scanned < maxScan {
		take := batchSize
		if rem := maxScan - scanned; rem < take {
			take = rem
		}
		if take <= 0 {
			break
		}
		batch, err := listFn(ctx, dbOffset, take)
		if err != nil {
			return VisiblePage{}, err
		}
		if len(batch) == 0 {
			return VisiblePage{Products: out}, nil
		}
		scanned += len(batch)

		ids := make([]string, len(batch))
		for i := range batch {
			ids[i] = batch[i].ID
		}
		byID, err := facts.LoadMany(ctx, ids)
		if err != nil {
			return VisiblePage{}, err
		}

		for i := range batch {
			resumeAt = dbOffset + i + 1
			snap, ok := byID[batch[i].ID]
			if !ok || !snap.Resolve(opts).AllowedOn(surface) {
				continue
			}
			if skipped < visibleSkip {
				skipped++
				continue
			}
			out = append(out, batch[i])
			if len(out) >= params.Limit {
				hasMore, err := repoHasMore(ctx, listFn, resumeAt)
				if err != nil {
					return VisiblePage{}, err
				}
				next := ""
				if hasMore {
					next = NextAfterCursor(resumeAt, 0)
					if next == "" {
						return VisiblePage{Products: out, DepthLimited: true}, nil
					}
				}
				return VisiblePage{
					Products:  out,
					HasMore:   hasMore,
					NextAfter: next,
				}, nil
			}
		}
		dbOffset += len(batch)
		resumeAt = dbOffset
		// Short batch means end of table when requests stay within the
		// repository Limit cap (visibilityScanMaxBatch == postgres maxListLimit).
		if len(batch) < take {
			return VisiblePage{Products: out}, nil
		}
	}

	if len(out) < params.Limit && scanned >= maxScan {
		hasMore, err := repoHasMore(ctx, listFn, resumeAt)
		if err != nil {
			return VisiblePage{}, err
		}
		if !hasMore {
			return VisiblePage{Products: out}, nil
		}
		remainingSkip := visibleSkip - skipped
		if remainingSkip < 0 {
			remainingSkip = 0
		}
		next := NextAfterCursor(resumeAt, remainingSkip)
		if next == "" {
			return VisiblePage{Products: out, Truncated: true, DepthLimited: true}, nil
		}
		return VisiblePage{
			Products:  out,
			Truncated: true,
			HasMore:   true,
			NextAfter: next,
		}, nil
	}
	return VisiblePage{Products: out}, nil
}

func repoHasMore(
	ctx context.Context,
	listFn func(ctx context.Context, dbOffset, dbLimit int) ([]Product, error),
	dbOffset int,
) (bool, error) {
	more, err := listFn(ctx, dbOffset, 1)
	if err != nil {
		return false, err
	}
	return len(more) > 0, nil
}
