package delta_test

import (
	"crypto/sha256"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
)

func TestCompareFunctions(t *testing.T) {
	t.Parallel()

	kept := function(".:Kept", "func Kept() {}")
	moved := function(".:Moved", "func Moved() {}")
	moved.File = "old.go"
	moved.Line = 10
	movedAfter := moved
	movedAfter.File = "new.go"
	movedAfter.Line = 42
	beforeChanged := function(".:Changed", "func Changed() { Old() }")
	afterChanged := function(".:Changed", "func Changed() { New() }")
	removed := function(".:Removed", "func Removed() {}")
	added := function(".:Added", "func Added() {}")
	before := inventory(removed, kept, beforeChanged, moved)
	after := inventory(movedAfter, added, afterChanged, kept)

	got := delta.Compare(before, after)
	require.Len(t, got.Functions, 5)
	assert.Equal(
		t,
		[]delta.FunctionChange{
			{ID: ".:Added", Status: delta.Added, After: &added},
			{ID: ".:Changed", Status: delta.Modified, Before: &beforeChanged, After: &afterChanged},
			{ID: ".:Kept", Status: delta.Unchanged, Before: &kept, After: &kept},
			{ID: ".:Moved", Status: delta.Unchanged, Before: &moved, After: &movedAfter},
			{ID: ".:Removed", Status: delta.Removed, Before: &removed},
		},
		got.Functions,
	)
	assert.Empty(t, got.Calls)
}

func TestCompareCalls(t *testing.T) {
	t.Parallel()

	before := inventory(function(".:A", "A"), function(".:B", "B"), function(".:Removed", "Removed"))
	after := inventory(function(".:A", "A"), function(".:B", "B"), function(".:Added", "Added"))
	before.Calls = []analysis.Call{
		{Caller: ".:Removed", Callee: ".:A"},
		{Caller: ".:A", Callee: ".:A"},
		{Caller: ".:B", Callee: ".:A"},
		{Caller: ".:A", Callee: ".:B"},
		{Caller: ".:A", Callee: ".:Removed"},
		{Caller: ".:A", Callee: ".:B"},
	}
	after.Calls = []analysis.Call{
		{Caller: ".:Added", Callee: ".:A"},
		{Caller: ".:A", Callee: ".:B"},
		{Caller: ".:A", Callee: ".:A"},
		{Caller: ".:A", Callee: ".:Added"},
		{Caller: ".:A", Callee: ".:Added"},
	}

	got := delta.Compare(before, after)
	assert.Equal(
		t,
		[]delta.CallChange{
			{Call: analysis.Call{Caller: ".:A", Callee: ".:A"}, Status: delta.CallRetained},
			{Call: analysis.Call{Caller: ".:A", Callee: ".:Added"}, Status: delta.CallAdded},
			{Call: analysis.Call{Caller: ".:A", Callee: ".:B"}, Status: delta.CallRetained},
			{Call: analysis.Call{Caller: ".:A", Callee: ".:Removed"}, Status: delta.CallRemoved},
			{Call: analysis.Call{Caller: ".:Added", Callee: ".:A"}, Status: delta.CallAdded},
			{Call: analysis.Call{Caller: ".:B", Callee: ".:A"}, Status: delta.CallRemoved},
			{Call: analysis.Call{Caller: ".:Removed", Callee: ".:A"}, Status: delta.CallRemoved},
		},
		got.Calls,
	)
}

func TestCompareResolutionDoesNotModifySourceStatus(t *testing.T) {
	t.Parallel()

	caller := function(".:Caller", "func Caller() { target() }")
	callerAfter := caller
	callerAfter.DynamicCalls = 1
	before := inventory(caller, function(".:Target", "Target"))
	after := inventory(callerAfter, function(".:Target", "Target"))
	before.Calls = []analysis.Call{{Caller: ".:Caller", Callee: ".:Target"}}

	got := delta.Compare(before, after)
	require.Len(t, got.Functions, 2)
	assert.Equal(t, delta.Unchanged, got.Functions[0].Status)
	assert.Equal(t, 0, got.Functions[0].Before.DynamicCalls)
	assert.Equal(t, 1, got.Functions[0].After.DynamicCalls)
	assert.Equal(
		t,
		[]delta.CallChange{
			{Call: analysis.Call{Caller: ".:Caller", Callee: ".:Target"}, Status: delta.CallRemoved},
		},
		got.Calls,
	)
}

func TestCompareEmptySides(t *testing.T) {
	t.Parallel()

	populated := inventory(function(".:A", "A"))
	populated.Calls = []analysis.Call{{Caller: ".:A", Callee: ".:A"}}
	cases := []struct {
		name   string
		before analysis.Snapshot
		after  analysis.Snapshot
		status delta.FunctionStatus
		call   delta.CallStatus
	}{
		{name: "added", after: populated, status: delta.Added, call: delta.CallAdded},
		{name: "removed", before: populated, status: delta.Removed, call: delta.CallRemoved},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := delta.Compare(test.before, test.after)
			require.Len(t, got.Functions, 1)
			require.Len(t, got.Calls, 1)
			assert.Equal(t, test.status, got.Functions[0].Status)
			assert.Equal(t, test.call, got.Calls[0].Status)
		})
	}

	empty := delta.Compare(analysis.Snapshot{}, analysis.Snapshot{})
	assert.Empty(t, empty.Functions)
	assert.Empty(t, empty.Calls)
}

func TestCompareOwnsFunctionCopies(t *testing.T) {
	t.Parallel()

	before := inventory(function(".:A", "A"), function(".:B", "B"))
	after := inventory(function(".:A", "Changed A"), function(".:B", "B"))
	before.Calls = []analysis.Call{{Caller: ".:B", Callee: ".:A"}, {Caller: ".:A", Callee: ".:B"}}
	after.Calls = []analysis.Call{{Caller: ".:A", Callee: ".:B"}}
	originalBefore := analysis.Snapshot{Functions: maps.Clone(before.Functions), Calls: slices.Clone(before.Calls)}
	originalAfter := analysis.Snapshot{Functions: maps.Clone(after.Functions), Calls: slices.Clone(after.Calls)}

	got := delta.Compare(before, after)
	require.Len(t, got.Functions, 2)
	require.NotNil(t, got.Functions[0].Before)
	require.NotNil(t, got.Functions[0].After)

	got.Functions[0].Before.Name = "mutated old copy"
	got.Functions[0].After.Name = "mutated new copy"
	assert.Equal(t, originalBefore, before)
	assert.Equal(t, originalAfter, after)
	assert.Equal(t, "B", got.Functions[1].Before.Name)
	assert.Equal(t, "B", got.Functions[1].After.Name)
}

func TestCompareDeterministic(t *testing.T) {
	t.Parallel()

	before := inventory(function(".:C", "C"), function(".:A", "A"), function(".:B", "B"))
	after := inventory(function(".:B", "B"), function(".:C", "New C"), function(".:A", "A"))
	before.Calls = []analysis.Call{{Caller: ".:C", Callee: ".:A"}, {Caller: ".:A", Callee: ".:B"}}
	after.Calls = []analysis.Call{{Caller: ".:B", Callee: ".:C"}, {Caller: ".:A", Callee: ".:B"}}
	first := delta.Compare(before, after)
	slices.Reverse(before.Calls)
	slices.Reverse(after.Calls)
	second := delta.Compare(before, after)
	assert.Equal(t, first, second)
}

func function(id analysis.ID, source string) analysis.Function {
	return analysis.Function{ID: id, Name: string(id)[2:], File: "source.go", Fingerprint: sha256.Sum256([]byte(source))}
}

func inventory(functions ...analysis.Function) analysis.Snapshot {
	result := analysis.Snapshot{Functions: make(map[analysis.ID]analysis.Function, len(functions))}
	for _, fn := range functions {
		result.Functions[fn.ID] = fn
	}

	return result
}
