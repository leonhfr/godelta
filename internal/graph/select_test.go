package graph_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
)

func TestSelectOneHopFromBothRevisions(t *testing.T) {
	t.Parallel()

	changes := delta.Delta{
		Functions: []delta.FunctionChange{
			function("Seed", delta.Modified),
			function("Caller", delta.Unchanged),
			function("OldCallee", delta.Unchanged),
			function("NewCallee", delta.Unchanged),
			function("TooFar", delta.Unchanged),
			function("Unrelated", delta.Unchanged),
		},
		Calls: []delta.CallChange{
			call("Caller", "Seed", delta.CallRetained),
			call("Seed", "OldCallee", delta.CallRemoved),
			call("Seed", "NewCallee", delta.CallAdded),
			call("NewCallee", "TooFar", delta.CallRetained),
			call("Caller", "OldCallee", delta.CallRetained),
		},
	}
	original := delta.Delta{Functions: slices.Clone(changes.Functions), Calls: slices.Clone(changes.Calls)}
	got := graph.Select(changes)
	require.Len(t, got.Components, 1)
	assert.Equal(t, []analysis.ID{"Caller", "NewCallee", "OldCallee", "Seed"}, ids(got.Components[0]))
	assert.Equal(
		t,
		[]delta.CallChange{
			call("Caller", "OldCallee", delta.CallRetained),
			call("Caller", "Seed", delta.CallRetained),
			call("Seed", "NewCallee", delta.CallAdded),
			call("Seed", "OldCallee", delta.CallRemoved),
		},
		got.Components[0].Calls,
	)
	assert.Equal(t, original, changes)
}

func TestSelectDoesNotConnectThroughUnselectedFunctions(t *testing.T) {
	t.Parallel()

	changes := delta.Delta{
		Functions: []delta.FunctionChange{
			function("A", delta.Added),
			function("B", delta.Removed),
			function("NearA", delta.Unchanged),
			function("Middle", delta.Unchanged),
			function("NearB", delta.Unchanged),
		},
		Calls: []delta.CallChange{
			call("A", "NearA", delta.CallAdded),
			call("NearA", "Middle", delta.CallRetained),
			call("Middle", "NearB", delta.CallRetained),
			call("NearB", "B", delta.CallRemoved),
		},
	}
	got := graph.Select(changes)
	require.Len(t, got.Components, 2)
	assert.Equal(t, []analysis.ID{"A", "NearA"}, ids(got.Components[0]))
	assert.Equal(t, []analysis.ID{"B", "NearB"}, ids(got.Components[1]))
}

func TestSelectContextBudgetPerComponent(t *testing.T) {
	t.Parallel()

	var changes delta.Delta
	for _, prefix := range []string{"a", "b"} {
		seed := analysis.ID(prefix + ":Seed")
		changes.Functions = append(changes.Functions, function(seed, delta.Modified))
		for index := range 55 {
			id := analysis.ID(fmt.Sprintf("%s:Context%02d", prefix, index))
			changes.Functions = append(changes.Functions, function(id, delta.Unchanged))
			changes.Calls = append(changes.Calls, call(seed, id, delta.CallRetained))
		}
	}

	got := graph.Select(changes)
	require.Len(t, got.Components, 2)

	for index, prefix := range []string{"a", "b"} {
		component := got.Components[index]
		require.Len(t, component.Functions, 51)
		assert.Len(t, component.Calls, 50)
		assert.Equal(t, 5, component.OmittedContext)
		assert.Equal(t, analysis.ID(prefix+":Context00"), component.Functions[0].ID)
		assert.Equal(t, analysis.ID(prefix+":Context49"), component.Functions[49].ID)
		assert.Equal(t, analysis.ID(prefix+":Seed"), component.Functions[50].ID)
	}
}

func TestSelectComponentsBeforeTruncation(t *testing.T) {
	t.Parallel()

	changes := delta.Delta{Functions: []delta.FunctionChange{function("SeedA", delta.Modified), function("SeedB", delta.Modified)}}
	for index := range 51 {
		id := analysis.ID(fmt.Sprintf("Context%02d", index))
		changes.Functions = append(changes.Functions, function(id, delta.Unchanged))
		changes.Calls = append(changes.Calls, call("SeedA", id, delta.CallRetained))
	}

	changes.Calls = append(changes.Calls, call("Context50", "SeedB", delta.CallRetained))
	got := graph.Select(changes)
	require.Len(t, got.Components, 1)
	assert.Len(t, got.Components[0].Functions, 52)
	assert.Equal(t, 1, got.Components[0].OmittedContext)
	assert.Contains(t, ids(got.Components[0]), analysis.ID("SeedB"))
	assert.NotContains(t, ids(got.Components[0]), analysis.ID("Context50"))
}

func TestSelectNeverDropsChangedFunctions(t *testing.T) {
	t.Parallel()

	var changes delta.Delta
	for index := range 60 {
		id := analysis.ID(fmt.Sprintf("Changed%02d", index))
		changes.Functions = append(changes.Functions, function(id, delta.Modified))
		if index != 0 {
			previous := analysis.ID(fmt.Sprintf("Changed%02d", index-1))
			changes.Calls = append(changes.Calls, call(previous, id, delta.CallRetained))
		}
	}

	got := graph.Select(changes)
	require.Len(t, got.Components, 1)
	assert.Len(t, got.Components[0].Functions, 60)
	assert.Len(t, got.Components[0].Calls, 59)
	assert.Zero(t, got.Components[0].OmittedContext)
}

func TestSelectNoSourceChanges(t *testing.T) {
	t.Parallel()

	changes := delta.Delta{
		Functions: []delta.FunctionChange{function("A", delta.Unchanged), function("B", delta.Unchanged)},
		Calls:     []delta.CallChange{call("A", "B", delta.CallRemoved)},
	}
	assert.Empty(t, graph.Select(changes).Components)
	assert.Empty(t, graph.Select(delta.Delta{}).Components)
}

func TestSelectDeterministicCyclesAndIsolatedNodes(t *testing.T) {
	t.Parallel()

	changes := delta.Delta{
		Functions: []delta.FunctionChange{function("C", delta.Added), function("B", delta.Unchanged), function("A", delta.Modified)},
		Calls:     []delta.CallChange{call("B", "A", delta.CallRetained), call("A", "B", delta.CallRetained), call("A", "A", delta.CallRetained)},
	}
	first := graph.Select(changes)
	slices.Reverse(changes.Functions)
	slices.Reverse(changes.Calls)
	assert.Equal(t, first, graph.Select(changes))
	require.Len(t, first.Components, 2)
	assert.Equal(t, []analysis.ID{"A", "B"}, ids(first.Components[0]))
	assert.Equal(t, []analysis.ID{"C"}, ids(first.Components[1]))
}

func function(id analysis.ID, status delta.FunctionStatus) delta.FunctionChange {
	source := analysis.Function{ID: id, Name: string(id), File: "source.go"}
	result := delta.FunctionChange{ID: id, Status: status}
	if status != delta.Added {
		result.Before = &source
	}

	if status != delta.Removed {
		after := source
		result.After = &after
	}

	return result
}

func call(caller, callee analysis.ID, status delta.CallStatus) delta.CallChange {
	return delta.CallChange{Call: analysis.Call{Caller: caller, Callee: callee}, Status: status}
}

func ids(component graph.Component) []analysis.ID {
	result := make([]analysis.ID, 0, len(component.Functions))
	for _, fn := range component.Functions {
		result = append(result, fn.ID)
	}

	return result
}
