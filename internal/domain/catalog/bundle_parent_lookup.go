package catalog

import "context"

// BundleParentLookupChunkSize is the batch size ExpandTouchedWithBundleParents
// uses for ListBundleIDsByComponentIDs. Matches stock-sync write chunks so a
// large CSV/ERP set is a handful of queries, not one per leaf.
const BundleParentLookupChunkSize = 100

// ExpandTouchedWithBundleParents merges parent bundle IDs for every key
// already in touched into the same map. Lookup failures for one chunk do
// not abort expansion of the others; the first error is returned after
// the pass so Trigger can still cover whatever was collected.
func ExpandTouchedWithBundleParents(ctx context.Context, lookup BundleParentLookup, touched map[string]struct{}) error {
	if lookup == nil || len(touched) == 0 {
		return nil
	}
	leaves := make([]string, 0, len(touched))
	seen := make(map[string]struct{}, len(touched))
	for id := range touched {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		leaves = append(leaves, id)
	}
	if len(leaves) == 0 {
		return nil
	}

	var firstErr error
	for i := 0; i < len(leaves); i += BundleParentLookupChunkSize {
		if err := ctx.Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		end := i + BundleParentLookupChunkSize
		if end > len(leaves) {
			end = len(leaves)
		}
		byLeaf, err := lookup.ListBundleIDsByComponentIDs(ctx, leaves[i:end])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for leafID, parents := range byLeaf {
			for _, parentID := range parents {
				if parentID == "" || parentID == leafID {
					continue
				}
				touched[parentID] = struct{}{}
			}
		}
	}
	return firstErr
}

// CollectBundleIDsByComponentIDs loops ListBundleIDsByComponentID. Test
// fakes that only store a per-component map use this so they satisfy
// BundleParentLookup without duplicating the loop.
func CollectBundleIDsByComponentIDs(ctx context.Context, lookup interface {
	ListBundleIDsByComponentID(ctx context.Context, componentProductID string) ([]string, error)
}, ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	if lookup == nil {
		return out, nil
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		parents, err := lookup.ListBundleIDsByComponentID(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(parents) > 0 {
			out[id] = parents
		}
	}
	return out, nil
}
