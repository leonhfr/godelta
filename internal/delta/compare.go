package delta

import (
	"cmp"
	"maps"
	"slices"

	"github.com/leonhfr/godelta/internal/analysis"
)

// Compare classifies declarations and calls without modifying either snapshot.
// Function changes depend only on identity and fingerprint, not source locations
// or call resolution. Repeated calls are treated as one relationship.
func Compare(before, after analysis.Snapshot) Delta {
	ids := make(map[analysis.ID]struct{}, len(before.Functions)+len(after.Functions))
	for id := range before.Functions {
		ids[id] = struct{}{}
	}

	for id := range after.Functions {
		ids[id] = struct{}{}
	}

	functions := make([]FunctionChange, 0, len(ids))
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		functions = append(functions, compareFunction(id, before, after))
	}

	return Delta{Functions: functions, Calls: compareCalls(before.Calls, after.Calls)}
}

func compareFunction(id analysis.ID, before, after analysis.Snapshot) FunctionChange {
	oldFunction, existed := before.Functions[id]
	newFunction, exists := after.Functions[id]
	change := FunctionChange{ID: id}
	if existed {
		change.Before = &oldFunction
	}

	if exists {
		change.After = &newFunction
	}

	switch {
	case !existed:
		change.Status = Added

	case !exists:
		change.Status = Removed

	case oldFunction.Fingerprint != newFunction.Fingerprint:
		change.Status = Modified

	default:
		change.Status = Unchanged
	}

	return change
}

func compareCalls(before, after []analysis.Call) []CallChange {
	oldCalls := make(map[analysis.Call]struct{}, len(before))
	statuses := make(map[analysis.Call]CallStatus, len(before)+len(after))
	for _, call := range before {
		oldCalls[call] = struct{}{}
		statuses[call] = CallRemoved
	}

	for _, call := range after {
		if _, retained := oldCalls[call]; retained {
			statuses[call] = CallRetained
		} else {
			statuses[call] = CallAdded
		}
	}

	calls := slices.SortedFunc(maps.Keys(statuses), func(left, right analysis.Call) int {
		if caller := cmp.Compare(left.Caller, right.Caller); caller != 0 {
			return caller
		}

		return cmp.Compare(left.Callee, right.Callee)
	})
	changes := make([]CallChange, 0, len(calls))
	for _, call := range calls {
		changes = append(changes, CallChange{Call: call, Status: statuses[call]})
	}

	return changes
}
