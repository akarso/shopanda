package plugin

import (
	"fmt"
	"sort"
)

// SortEntriesByDependsOn topologically sorts registry entries using dependsOn[name] lists.
// Names must match plugin Name() values. Returns error on cycle or missing dependency.
func SortEntriesByDependsOn(entries []Entry, dependsOn map[string][]string) ([]Entry, error) {
	if len(entries) == 0 || len(dependsOn) == 0 {
		return entries, nil
	}
	byName := make(map[string]Entry, len(entries))
	order := make(map[string]int, len(entries))
	for i, e := range entries {
		byName[e.Name] = e
		order[e.Name] = i
	}

	inDegree := make(map[string]int, len(entries))
	dependents := make(map[string][]string)
	for name := range byName {
		inDegree[name] = 0
	}
	for name, deps := range dependsOn {
		if _, ok := byName[name]; !ok {
			continue
		}
		for _, dep := range deps {
			if dep == "" {
				continue
			}
			if _, ok := byName[dep]; !ok {
				return nil, fmt.Errorf("plugin depends_on: %q depends on unknown plugin %q", name, dep)
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	queue := make([]string, 0)
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return order[queue[i]] < order[queue[j]] })

	sorted := make([]Entry, 0, len(entries))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byName[name])
		for _, child := range dependents[name] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
		sort.SliceStable(queue, func(i, j int) bool { return order[queue[i]] < order[queue[j]] })
	}
	if len(sorted) != len(entries) {
		return nil, fmt.Errorf("plugin depends_on: cycle detected")
	}
	return sorted, nil
}
