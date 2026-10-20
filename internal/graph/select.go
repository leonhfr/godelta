// Package graph selects review context around changed Go functions.
package graph

import (
	"cmp"
	"maps"
	"slices"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
)

// ContextLimit caps unchanged functions per connected component.
const ContextLimit = 50

// Component groups selected functions before context truncation.
type Component struct {
	Functions      []delta.FunctionChange
	Calls          []delta.CallChange
	OmittedContext int
}

// Graph contains only changed functions and their immediate call neighbors.
type Graph struct {
	Components []Component
}

// Select includes callers and callees from both revisions without recursive expansion.
// It keeps every changed function and sorts unchanged context by canonical identity.
func Select(changes delta.Delta) Graph {
	functions := make(map[analysis.ID]delta.FunctionChange, len(changes.Functions))
	seeds := make(map[analysis.ID]bool)
	for _, fn := range changes.Functions {
		functions[fn.ID] = fn
		if fn.Status != delta.Unchanged {
			seeds[fn.ID] = true
		}
	}

	selected := maps.Clone(seeds)
	for _, edge := range changes.Calls {
		if seeds[edge.Call.Caller] {
			selected[edge.Call.Callee] = true
		}

		if seeds[edge.Call.Callee] {
			selected[edge.Call.Caller] = true
		}
	}

	adjacency := make(map[analysis.ID][]analysis.ID)
	for _, edge := range changes.Calls {
		if selected[edge.Call.Caller] && selected[edge.Call.Callee] {
			adjacency[edge.Call.Caller] = append(adjacency[edge.Call.Caller], edge.Call.Callee)
			adjacency[edge.Call.Callee] = append(adjacency[edge.Call.Callee], edge.Call.Caller)
		}
	}

	var result Graph
	visited := make(map[analysis.ID]bool)
	for _, root := range slices.Sorted(maps.Keys(selected)) {
		if visited[root] {
			continue
		}

		ids := connected(root, adjacency, visited)
		result.Components = append(result.Components, limitComponent(ids, functions, changes.Calls))
	}

	return result
}

func connected(root analysis.ID, adjacency map[analysis.ID][]analysis.ID, visited map[analysis.ID]bool) []analysis.ID {
	queue := []analysis.ID{root}
	visited[root] = true
	for index := 0; index < len(queue); index++ {
		for _, neighbor := range adjacency[queue[index]] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	slices.Sort(queue)

	return queue
}

func limitComponent(ids []analysis.ID, functions map[analysis.ID]delta.FunctionChange, calls []delta.CallChange) Component {
	var component Component
	kept := make(map[analysis.ID]bool)
	contextCount := 0
	for _, id := range ids {
		fn := functions[id]
		if fn.Status == delta.Unchanged {
			if contextCount == ContextLimit {
				component.OmittedContext++
				continue
			}

			contextCount++
		}

		kept[id] = true
		component.Functions = append(component.Functions, fn)
	}

	for _, edge := range calls {
		if kept[edge.Call.Caller] && kept[edge.Call.Callee] {
			component.Calls = append(component.Calls, edge)
		}
	}

	slices.SortFunc(component.Calls, func(left, right delta.CallChange) int {
		if caller := cmp.Compare(left.Call.Caller, right.Call.Caller); caller != 0 {
			return caller
		}

		return cmp.Compare(left.Call.Callee, right.Call.Callee)
	})

	return component
}
