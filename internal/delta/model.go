// Package delta compares function inventories and call relationships between revisions.
package delta

import "github.com/leonhfr/godelta/internal/analysis"

// FunctionStatus describes a declaration's presence and normalized source changes.
type FunctionStatus string

const (
	// Added identifies a function present only in the new snapshot.
	Added FunctionStatus = "added"
	// Removed identifies a function present only in the old snapshot.
	Removed FunctionStatus = "removed"
	// Modified identifies a matched function with changed normalized source.
	Modified FunctionStatus = "modified"
	// Unchanged identifies a matched function with the same normalized source.
	Unchanged FunctionStatus = "unchanged"
)

// CallStatus describes membership in the old and new call graphs.
type CallStatus string

const (
	// CallAdded identifies a call present only in the new snapshot.
	CallAdded CallStatus = "added"
	// CallRemoved identifies a call present only in the old snapshot.
	CallRemoved CallStatus = "removed"
	// CallRetained identifies a call present in both snapshots.
	CallRetained CallStatus = "retained"
)

// FunctionChange preserves both versions of a function matched by its identity.
// Before is nil for added functions; After is nil for removed functions.
type FunctionChange struct {
	ID     analysis.ID
	Status FunctionStatus
	Before *analysis.Function
	After  *analysis.Function
}

// CallChange classifies one caller-to-callee relationship across snapshots.
type CallChange struct {
	Call   analysis.Call
	Status CallStatus
}

// Delta contains the complete union of both snapshots, including unchanged context.
// Functions are sorted by ID; calls are sorted by caller then callee.
type Delta struct {
	Functions []FunctionChange
	Calls     []CallChange
}
