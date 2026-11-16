package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

// MaxLookupIDs caps batch catalog reads (same order of magnitude as list
// limits elsewhere — e.g. variant list max 100). Callers that exceed this
// get an error; the tail is not dropped.
const MaxLookupIDs = 100

// ErrTooManyLookupIDs is returned when NormalizeLookupIDs sees more than
// MaxLookupIDs distinct non-empty ids.
var ErrTooManyLookupIDs = fmt.Errorf("catalog: at most %d ids per lookup", MaxLookupIDs)

// VariantBatchLookup is an optional batch read (postgres FindByIDs).
type VariantBatchLookup interface {
	FindByIDs(ctx context.Context, ids []string) (map[string]*Variant, error)
}

// ProductBatchLookup is an optional batch read (postgres ListByIDs).
type ProductBatchLookup interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*Product, error)
}

// NormalizeLookupIDs trims, dedupes, and drops blanks. Returns
// ErrTooManyLookupIDs when more than MaxLookupIDs distinct ids remain.
func NormalizeLookupIDs(ids []string) ([]string, error) {
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
		if len(clean) > MaxLookupIDs {
			return nil, ErrTooManyLookupIDs
		}
	}
	return clean, nil
}

// LookupVariants loads variants keyed by the requested id. Uses FindByIDs when
// the repository implements VariantBatchLookup. Falls back to per-id FindByID
// only when the batch error is apperror.Validation (e.g. one bad UUID in
// ANY($1)); other batch errors (timeout, connection) propagate immediately.
// On the per-id pass, validation errors for individual ids are skipped.
func LookupVariants(ctx context.Context, repo VariantRepository, ids []string) (map[string]*Variant, error) {
	if repo == nil {
		return nil, fmt.Errorf("catalog: variants repository not configured")
	}
	clean, err := NormalizeLookupIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(clean) == 0 {
		return map[string]*Variant{}, nil
	}
	if batch, ok := repo.(VariantBatchLookup); ok {
		found, err := batch.FindByIDs(ctx, clean)
		if err == nil {
			return rekeyVariants(clean, found), nil
		}
		if !apperror.Is(err, apperror.CodeValidation) {
			return nil, err
		}
	}
	out := make(map[string]*Variant, len(clean))
	for _, id := range clean {
		v, err := repo.FindByID(ctx, id)
		if err != nil {
			if apperror.Is(err, apperror.CodeValidation) {
				continue
			}
			return nil, fmt.Errorf("catalog: lookup variant %q: %w", id, err)
		}
		if v != nil {
			out[id] = v
		}
	}
	return out, nil
}

// LookupProducts loads products keyed by the requested id. Uses ListByIDs when
// available; falls back to per-id only on apperror.Validation batch errors.
// A nil repository returns an empty map. Non-active parents follow the same
// IncludeNonActiveProducts scope as FindByID.
func LookupProducts(ctx context.Context, repo ProductRepository, ids []string) (map[string]*Product, error) {
	if repo == nil {
		return map[string]*Product{}, nil
	}
	clean, err := NormalizeLookupIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(clean) == 0 {
		return map[string]*Product{}, nil
	}
	if batch, ok := repo.(ProductBatchLookup); ok {
		found, err := batch.ListByIDs(ctx, clean)
		if err == nil {
			return rekeyProducts(clean, found), nil
		}
		if !apperror.Is(err, apperror.CodeValidation) {
			return nil, err
		}
	}
	out := make(map[string]*Product, len(clean))
	for _, id := range clean {
		p, err := repo.FindByID(ctx, id)
		if err != nil {
			if apperror.Is(err, apperror.CodeValidation) {
				continue
			}
			return nil, fmt.Errorf("catalog: lookup product %q: %w", id, err)
		}
		if p != nil {
			out[id] = p
		}
	}
	return out, nil
}

func rekeyVariants(requested []string, found map[string]*Variant) map[string]*Variant {
	if found == nil {
		return map[string]*Variant{}
	}
	out := make(map[string]*Variant, len(requested))
	for _, reqID := range requested {
		if v := found[reqID]; v != nil {
			out[reqID] = v
			continue
		}
		for _, v := range found {
			if v != nil && strings.EqualFold(v.ID, reqID) {
				out[reqID] = v
				break
			}
		}
	}
	return out
}

func rekeyProducts(requested []string, found map[string]*Product) map[string]*Product {
	if found == nil {
		return map[string]*Product{}
	}
	out := make(map[string]*Product, len(requested))
	for _, reqID := range requested {
		if p := found[reqID]; p != nil {
			out[reqID] = p
			continue
		}
		for _, p := range found {
			if p != nil && strings.EqualFold(p.ID, reqID) {
				out[reqID] = p
				break
			}
		}
	}
	return out
}
